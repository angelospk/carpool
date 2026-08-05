package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// geoJSONFile is the subset of GeoJSON the region seeder reads: a
// FeatureCollection whose features carry a name and a polygonal geometry.
type geoJSONFile struct {
	Features []struct {
		Properties struct {
			Name string `json:"name"`
			// PreviousName is what this same ground was called by an earlier
			// fixture. Seeding is keyed on the name, so without it the country
			// fixture would insert "Περιφερειακή Ενότητα Έβρου" alongside the
			// pilot's "Έβρος" — two rows for one prefecture, with every account
			// created during the pilot still pointing at the orphan.
			PreviousName string `json:"previous_name"`
		} `json:"properties"`
		Geometry json.RawMessage `json:"geometry"`
	} `json:"features"`
}

// SeedRegionsFromFile loads region boundaries from a GeoJSON FeatureCollection.
//
// Seeding is idempotent and keyed on the region name, so redeploying does not
// duplicate regions and an updated boundary or fuel price is picked up.
func SeedRegionsFromFile(ctx context.Context, pool *pgxpool.Pool, path string, fuelPriceEUR float64) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("store: reading region fixture: %w", err)
	}

	var parsed geoJSONFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("store: parsing region fixture: %w", err)
	}
	if len(parsed.Features) == 0 {
		return fmt.Errorf("store: region fixture %s contains no features", path)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, f := range parsed.Features {
		if f.Properties.Name == "" {
			return fmt.Errorf("store: region fixture %s has a feature without a name", path)
		}

		// Adopt the row this region used to be, if it is still under its old
		// name. Guarded so that a rename which has already happened, or a
		// database that somehow holds both, does not collide with the unique
		// index and abort the whole seed.
		if p := f.Properties.PreviousName; p != "" {
			_, err := tx.Exec(ctx, `
				UPDATE regions SET name = $1, updated_at = now()
				WHERE name = $2
				  AND NOT EXISTS (SELECT 1 FROM regions WHERE name = $1)`,
				f.Properties.Name, p)
			if err != nil {
				return fmt.Errorf("store: renaming region %q to %q: %w", p, f.Properties.Name, err)
			}
		}

		// ST_Multi normalises Polygon and MultiPolygon to the column's type, and
		// ST_MakeValid repairs the self-intersections that raw OSM boundaries
		// occasionally carry.
		_, err := tx.Exec(ctx, `
			INSERT INTO regions (name, boundary, fuel_price_eur)
			VALUES ($1,
			        ST_Multi(ST_MakeValid(ST_SetSRID(ST_GeomFromGeoJSON($2), 4326))),
			        $3)
			ON CONFLICT (name) DO UPDATE
			SET boundary       = EXCLUDED.boundary,
			    fuel_price_eur = EXCLUDED.fuel_price_eur,
			    -- Only when the figure actually moves. A reseed that reasserts
			    -- the same price has not made it any fresher, and stamping it
			    -- anyway would hide exactly the staleness the column exists to
			    -- show.
			    fuel_price_updated_at = CASE
			        WHEN regions.fuel_price_eur IS DISTINCT FROM EXCLUDED.fuel_price_eur
			        THEN now() ELSE regions.fuel_price_updated_at END,
			    updated_at     = now()`,
			f.Properties.Name, string(f.Geometry), fuelPriceEUR)
		if err != nil {
			return fmt.Errorf("store: seeding region %q: %w", f.Properties.Name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

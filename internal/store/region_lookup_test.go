package store_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

func greecePath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "fixtures", "regions", "greece.geojson"))
	if err != nil {
		t.Fatalf("resolving fixture path: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture missing: %v", err)
	}
	return p
}

func seededGreece(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := store.SeedRegionsFromFile(ctx, pool, greecePath(t), 1.800); err != nil {
		t.Fatalf("SeedRegionsFromFile: %v", err)
	}
	return ctx, pool
}

// The whole country, not one prefecture. 74 περιφερειακές ενότητες: the 51
// νομοί as they were, with Αττική split into eight and a handful of islands
// standing on their own.
func TestSeedingGreeceLoadsEveryRegionalUnit(t *testing.T) {
	ctx, pool := seededGreece(t)

	var count, invalid int
	err := pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE NOT ST_IsValid(boundary))
		FROM regions`).Scan(&count, &invalid)
	if err != nil {
		t.Fatalf("counting regions: %v", err)
	}
	// 74 περιφερειακές ενότητες, plus Άγιο Όρος, which belongs to none of them.
	if count != 75 {
		t.Errorf("seeded %d regions, want 75", count)
	}
	if invalid != 0 {
		t.Errorf("%d seeded boundaries are not valid geometries", invalid)
	}
}

// The lookup that replaces asking somebody which prefecture they live in.
func TestRegionAtNamesTheUnitAPointFallsIn(t *testing.T) {
	ctx, pool := seededGreece(t)

	tests := []struct {
		place string
		lon   float64
		lat   float64
		want  string
	}{
		{"Φέρες", 26.1740, 40.8940, "Περιφερειακή Ενότητα Έβρου"},
		{"Θεσσαλονίκη", 22.9444, 40.6401, "Μητροπολιτική Ενότητα Θεσσαλονίκης"},
		{"Σύνταγμα, Αθήνα", 23.7348, 37.9755, "Περιφερειακή Ενότητα Κεντρικού Τομέα Αθηνών"},
		{"Ιωάννινα", 20.8537, 39.6650, "Περιφερειακή Ενότητα Ιωαννίνων"},
		{"Ηράκλειο", 25.1442, 35.3387, "Περιφερειακή Ενότητα Ηρακλείου"},
		// Not a prefecture capital: a village, which is what most pickups are.
		{"Νυμφαίο", 21.4938, 40.6442, "Περιφερειακή Ενότητα Φλώρινας"},
		// In no περιφερειακή ενότητα at all, and a 330 km² hole in the middle
		// of Χαλκιδική if it is left out.
		{"Καρυές Αγίου Όρους", 24.2450, 40.2570, "Αυτόνομη Μοναστική Πολιτεία Αγίου Όρους"},
		// A berth, not a town. The boundaries carry territorial waters, so a
		// pin dropped on the water where the ferry ties up still lands in
		// Αττική rather than nowhere.
		{"το μουράγιο στη Ραφήνα", 24.0089, 38.0231, "Περιφερειακή Ενότητα Ανατολικής Αττικής"},
	}

	for _, tt := range tests {
		t.Run(tt.place, func(t *testing.T) {
			got, err := store.RegionAt(ctx, pool, tt.lon, tt.lat)
			if err != nil {
				t.Fatalf("RegionAt: %v", err)
			}
			if got.Name != tt.want {
				t.Errorf("RegionAt(%s) = %q, want %q", tt.place, got.Name, tt.want)
			}
		})
	}
}

// The seam.
//
// Rings are simplified one at a time, so two neighbours stop meeting along
// exactly the same line and a gap of a couple of hundred metres can open
// between them. Greece as currently drawn has no such gap — which is precisely
// why this test draws its own rather than pointing at a real village and
// hoping. A square region, and a point just outside it.
func TestRegionAtReachesAcrossASeamButNotAcrossABorder(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// A degree of longitude at this latitude is about 85 km, so the square runs
	// from 25.00 to 25.10 — roughly 8.5 km across.
	fixture := filepath.Join(t.TempDir(), "square.geojson")
	square := `{"type":"FeatureCollection","features":[{"type":"Feature",
		"properties":{"name":"Τετράγωνο"},
		"geometry":{"type":"Polygon","coordinates":[[
			[25.00,41.00],[25.10,41.00],[25.10,41.10],[25.00,41.10],[25.00,41.00]]]}}]}`
	if err := os.WriteFile(fixture, []byte(square), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	if err := store.SeedRegionsFromFile(ctx, pool, fixture, 1.800); err != nil {
		t.Fatalf("SeedRegionsFromFile: %v", err)
	}

	// About 850 m west of the edge: the width of a seam.
	if got, err := store.RegionAt(ctx, pool, 24.99, 41.05); err != nil {
		t.Errorf("a point in the seam: %v", err)
	} else if got.Name != "Τετράγωνο" {
		t.Errorf("a point in the seam resolved to %q", got.Name)
	}

	// About 8.5 km west: no longer a rounding error.
	if _, err := store.RegionAt(ctx, pool, 24.90, 41.05); !errors.Is(err, store.ErrOutsideGreece) {
		t.Errorf("a point 8.5 km out gave %v, want ErrOutsideGreece", err)
	}
}

// The fallback repairs gaps inside Greece. It must not quietly adopt Sofia:
// the app does not serve Bulgaria, and a nearest-match there would hand
// somebody a Greek fuel price for a country they are not in.
func TestRegionAtRefusesAPointOutsideTheCountry(t *testing.T) {
	ctx, pool := seededGreece(t)

	for _, tt := range []struct {
		place string
		lon   float64
		lat   float64
	}{
		{"Σόφια", 23.3219, 42.6977},
		{"Κωνσταντινούπολη", 28.9784, 41.0082},
		{"ανοιχτά, στο Λιβυκό", 21.0000, 34.5000},
	} {
		t.Run(tt.place, func(t *testing.T) {
			_, err := store.RegionAt(ctx, pool, tt.lon, tt.lat)
			if !errors.Is(err, store.ErrOutsideGreece) {
				t.Errorf("RegionAt(%s) error = %v, want ErrOutsideGreece", tt.place, err)
			}
		})
	}
}

// The pilot's one region was called Έβρος. Seeding the country would otherwise
// add a second row for the same ground under its OSM name, and every account
// created during the pilot would keep pointing at the orphan.
func TestSeedingGreeceAdoptsThePilotRegionRatherThanDuplicatingIt(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	evros, err := filepath.Abs(filepath.Join("..", "..", "fixtures", "regions", "evros.geojson"))
	if err != nil {
		t.Fatalf("resolving the pilot fixture: %v", err)
	}
	if err := store.SeedRegionsFromFile(ctx, pool, evros, 1.800); err != nil {
		t.Fatalf("seeding the pilot region: %v", err)
	}

	var pilotID string
	if err := pool.QueryRow(ctx, `SELECT id FROM regions`).Scan(&pilotID); err != nil {
		t.Fatalf("reading the pilot region: %v", err)
	}

	if err := store.SeedRegionsFromFile(ctx, pool, greecePath(t), 1.800); err != nil {
		t.Fatalf("seeding the country: %v", err)
	}

	// The count is the load-bearing assertion. Both rows would cover Φέρες, and
	// the lookup would happily return the pilot's — so checking only the id
	// passes while a duplicate sits in the table waiting to be somebody's home.
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM regions`).Scan(&count); err != nil {
		t.Fatalf("counting regions: %v", err)
	}
	if count != 75 {
		t.Errorf("%d regions after seeding the country over the pilot, want 75", count)
	}

	got, err := store.RegionAt(ctx, pool, 26.1740, 40.8940)
	if err != nil {
		t.Fatalf("RegionAt(Φέρες): %v", err)
	}
	if got.ID.String() != pilotID {
		t.Errorf("Έβρος came back as a new region %s, leaving the pilot's %s orphaned", got.ID, pilotID)
	}
	if got.Name != "Περιφερειακή Ενότητα Έβρου" {
		t.Errorf("the adopted row kept the old name %q", got.Name)
	}
}

package store_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

func fixturePath(t *testing.T) string {
	t.Helper()
	// The test binary runs in the package directory; the fixture lives at the
	// repository root.
	p, err := filepath.Abs(filepath.Join("..", "..", "fixtures", "regions", "evros.geojson"))
	if err != nil {
		t.Fatalf("resolving fixture path: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture missing: %v", err)
	}
	return p
}

func TestSeedRegionsLoadsTheGeoJSONFixture(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if err := store.SeedRegionsFromFile(ctx, pool, fixturePath(t), 1.800); err != nil {
		t.Fatalf("SeedRegionsFromFile: %v", err)
	}

	var name string
	var valid bool
	err := pool.QueryRow(ctx,
		`SELECT name, ST_IsValid(boundary) FROM regions`).Scan(&name, &valid)
	if err != nil {
		t.Fatalf("reading seeded region: %v", err)
	}
	if name != "Έβρος" {
		t.Errorf("region name = %q, want Έβρος", name)
	}
	if !valid {
		t.Error("the seeded boundary is not a valid geometry")
	}
}

func TestSeededRegionContainsAlexandroupoliAndExcludesThessaloniki(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := store.SeedRegionsFromFile(ctx, pool, fixturePath(t), 1.800); err != nil {
		t.Fatalf("SeedRegionsFromFile: %v", err)
	}

	tests := []struct {
		name   string
		lon    float64
		lat    float64
		inside bool
	}{
		{"Alexandroupoli, the region capital", 25.8743, 40.8476, true},
		{"Orestiada, in the north of the region", 26.5300, 41.5040, true},
		{"Thessaloniki, far to the west", 22.9444, 40.6401, false},
		{"Komotini, the neighbouring region", 25.4058, 41.1222, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			err := pool.QueryRow(ctx,
				`SELECT ST_Contains(boundary, ST_SetSRID(ST_MakePoint($1, $2), 4326)) FROM regions`,
				tt.lon, tt.lat).Scan(&got)
			if err != nil {
				t.Fatalf("containment query: %v", err)
			}
			if got != tt.inside {
				t.Fatalf("ST_Contains = %v, want %v", got, tt.inside)
			}
		})
	}
}

func TestSeedRegionsIsIdempotent(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for i := range 2 {
		if err := store.SeedRegionsFromFile(ctx, pool, fixturePath(t), 1.800); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM regions`).Scan(&count); err != nil {
		t.Fatalf("counting regions: %v", err)
	}
	if count != 1 {
		t.Fatalf("region count = %d, want 1 after seeding twice", count)
	}
}

func TestSeedRegionsUpdatesTheFuelPriceOnReseed(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := store.SeedRegionsFromFile(ctx, pool, fixturePath(t), 1.800); err != nil {
		t.Fatalf("first seed: %v", err)
	}

	if err := store.SeedRegionsFromFile(ctx, pool, fixturePath(t), 1.950); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	var price float64
	if err := pool.QueryRow(ctx, `SELECT fuel_price_eur FROM regions`).Scan(&price); err != nil {
		t.Fatalf("reading fuel price: %v", err)
	}
	if price != 1.950 {
		t.Fatalf("fuel_price_eur = %v, want 1.950", price)
	}
}

// One number per region, edited by hand, wrong for everybody at once when it
// goes stale — and until now nothing recorded when it was last touched.
//
// The subtlety worth a test: a reseed that reasserts the same price has not
// refreshed anything. Stamping it anyway would make a six-month-old figure look
// current, which is worse than having no timestamp at all.
func TestTheFuelPriceTimestampMovesOnlyWhenThePriceDoes(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	stamp := func() time.Time {
		t.Helper()
		var at time.Time
		if err := pool.QueryRow(ctx,
			`SELECT fuel_price_updated_at FROM regions WHERE name = 'Έβρος'`).Scan(&at); err != nil {
			t.Fatalf("reading the stamp: %v", err)
		}
		return at
	}

	if err := store.SeedRegionsFromFile(ctx, pool, fixturePath(t), 1.800); err != nil {
		t.Fatalf("SeedRegions: %v", err)
	}
	first := stamp()

	if err := store.SeedRegionsFromFile(ctx, pool, fixturePath(t), 1.800); err != nil {
		t.Fatalf("reseeding the same price: %v", err)
	}
	if again := stamp(); !again.Equal(first) {
		t.Errorf("reasserting the same price moved the stamp %v -> %v, so a stale figure now looks fresh",
			first, again)
	}

	if err := store.SeedRegionsFromFile(ctx, pool, fixturePath(t), 1.905); err != nil {
		t.Fatalf("reseeding a new price: %v", err)
	}
	if moved := stamp(); !moved.After(first) {
		t.Errorf("the price changed but the stamp stayed at %v", moved)
	}
}

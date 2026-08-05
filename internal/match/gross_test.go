package match_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/match"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

// The fixtures below are laid out around Alexandroupoli, in the Evros region.
// A degree of longitude at 41°N is about 84 km and a degree of latitude about
// 111 km, so the coordinates are chosen to make the distances obvious:
//
//	corridor  — a straight west→east line along latitude 40.85 from lon 25.80
//	            to 26.00, roughly 17 km long.
//	The passenger's origin and destination sit just off that line unless a test
//	says otherwise.
const (
	corridorWKT = "LINESTRING(25.80 40.85, 26.00 40.85)"
	// The reverse corridor traverses exactly the same ground east→west.
	reverseCorridorWKT = "LINESTRING(26.00 40.85, 25.80 40.85)"
	// A crossing route runs south→north through the middle of the corridor,
	// sharing only the crossing point.
	crossingWKT = "LINESTRING(25.90 40.75, 25.90 40.95)"
)

type fixture struct {
	pool     *pgxpool.Pool
	regionID uuid.UUID
	driverID uuid.UUID
}

func setup(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	f := &fixture{pool: pool}

	// A crude region box around Evros is enough; admissibility is enforced at
	// creation time, not during matching.
	err := pool.QueryRow(ctx, `
		INSERT INTO regions (name, boundary)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25.3 40.3, 26.7 40.3, 26.7 41.8, 25.3 41.8, 25.3 40.3))', 4326)))
		RETURNING id`).Scan(&f.regionID)
	if err != nil {
		t.Fatalf("seeding region: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO fuel_prices (region_id, fuel, price_eur, source)
		VALUES (NULL, 'petrol', 1.859, 'test'), (NULL, 'diesel', 1.729, 'test')`); err != nil {
		t.Fatalf("seeding fuel prices: %v", err)
	}

	err = pool.QueryRow(ctx, `
		INSERT INTO users (phone, name, home_region_id)
		VALUES ('+306900000001', 'Driver', $1) RETURNING id`, f.regionID).Scan(&f.driverID)
	if err != nil {
		t.Fatalf("seeding driver user: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO driver_profiles (user_id, car_model, car_plate, notify_radius_km, detour_budget_min)
		VALUES ($1, 'Yaris', 'ABC-1234', 2.0, 10)`, f.driverID)
	if err != nil {
		t.Fatalf("seeding driver profile: %v", err)
	}

	return f
}

// addOccurrence inserts a template plus one scheduled occurrence on the given
// route line, and returns the occurrence id.
func (f *fixture) addOccurrence(t *testing.T, routeWKT string, opts ...func(*occurrenceOpts)) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	o := occurrenceOpts{
		departEarliest: time.Date(2026, time.July, 30, 7, 0, 0, 0, time.UTC),
		departLatest:   time.Date(2026, time.July, 30, 8, 0, 0, 0, time.UTC),
		seatsFree:      3,
		status:         "scheduled",
		routeKm:        17.0,
	}
	for _, opt := range opts {
		opt(&o)
	}

	var templateID uuid.UUID
	err := f.pool.QueryRow(ctx, `
		INSERT INTO route_templates (
			driver_id, origin, destination, origin_label, destination_label,
			route_geom, route_km, route_min, osrm_data_version)
		VALUES ($1,
			ST_StartPoint(ST_GeomFromText($2, 4326)),
			ST_EndPoint(ST_GeomFromText($2, 4326)),
			'A', 'B',
			ST_GeomFromText($2, 4326), $3, 20, 'test')
		RETURNING id`, f.driverID, routeWKT, o.routeKm).Scan(&templateID)
	if err != nil {
		t.Fatalf("inserting route template: %v", err)
	}

	var occurrenceID uuid.UUID
	err = f.pool.QueryRow(ctx, `
		INSERT INTO trip_occurrences (
			template_id, service_date, depart_earliest, depart_latest,
			seats_total, seats_free, status, template_revision,
			route_geom, route_km, route_min, pricing_mode)
		VALUES ($1, $2::date, $3, $4, 3, $5, $6, 1,
			ST_GeomFromText($7, 4326), $8, 20, 'formula')
		RETURNING id`,
		templateID, o.departEarliest.Format("2006-01-02"), o.departEarliest, o.departLatest,
		o.seatsFree, o.status, routeWKT, o.routeKm).Scan(&occurrenceID)
	if err != nil {
		t.Fatalf("inserting occurrence: %v", err)
	}
	return occurrenceID
}

type occurrenceOpts struct {
	departEarliest time.Time
	departLatest   time.Time
	seatsFree      int
	status         string
	routeKm        float64
}

func withSeatsFree(n int) func(*occurrenceOpts) {
	return func(o *occurrenceOpts) { o.seatsFree = n }
}
func withStatus(s string) func(*occurrenceOpts) {
	return func(o *occurrenceOpts) { o.status = s }
}
func withWindow(start, end time.Time) func(*occurrenceOpts) {
	return func(o *occurrenceOpts) { o.departEarliest, o.departLatest = start, end }
}

// query is a request travelling west→east along the corridor: pickup near the
// western end, dropoff near the eastern end.
func query() match.GrossQuery {
	return match.GrossQuery{
		Origin:      match.Point{Lon: 25.82, Lat: 40.851},
		Destination: match.Point{Lon: 25.98, Lat: 40.851},
		WindowStart: time.Date(2026, time.July, 30, 6, 30, 0, 0, time.UTC),
		WindowEnd:   time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC),
		Seats:       1,
		RequestKm:   13.5,
	}
}

func TestGrossMatchAcceptsASameDirectionRoute(t *testing.T) {
	f := setup(t)
	want := f.addOccurrence(t, corridorWKT)

	got, err := match.Gross(context.Background(), f.pool, query())
	if err != nil {
		t.Fatalf("Gross: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1: %+v", len(got), got)
	}
	if got[0].OccurrenceID != want {
		t.Errorf("candidate = %v, want %v", got[0].OccurrenceID, want)
	}
	if !(got[0].FractionOrigin < got[0].FractionDest) {
		t.Errorf("pickup fraction %v should precede dropoff fraction %v",
			got[0].FractionOrigin, got[0].FractionDest)
	}
}

func TestGrossMatchRejectsAReverseDirectionRoute(t *testing.T) {
	f := setup(t)
	f.addOccurrence(t, reverseCorridorWKT)

	got, err := match.Gross(context.Background(), f.pool, query())
	if err != nil {
		t.Fatalf("Gross: %v", err)
	}

	// The buffer overlap is total — only the along-line ordering check can
	// reject this, which is exactly why it exists.
	if len(got) != 0 {
		t.Fatalf("a route driven in the opposite direction must be rejected, got %+v", got)
	}
}

func TestGrossMatchRejectsAPerpendicularCrossingRoute(t *testing.T) {
	f := setup(t)
	f.addOccurrence(t, crossingWKT)

	got, err := match.Gross(context.Background(), f.pool, query())
	if err != nil {
		t.Fatalf("Gross: %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("a route merely crossing the corridor must be rejected, got %+v", got)
	}
}

func TestGrossMatchRejectsAnOverlapBelowTheSharedFractionFloor(t *testing.T) {
	f := setup(t)
	// The driver's route covers only the first ~1.7 km of the corridor, so the
	// shared distance falls far below max(1 km, 20% of the 13.5 km request).
	f.addOccurrence(t, "LINESTRING(25.80 40.85, 25.82 40.85)", func(o *occurrenceOpts) { o.routeKm = 1.7 })

	got, err := match.Gross(context.Background(), f.pool, query())
	if err != nil {
		t.Fatalf("Gross: %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("an overlap below the shared-distance floor must be rejected, got %+v", got)
	}
}

func TestGrossMatchRejectsAPickupBeyondTheDriverNotifyRadius(t *testing.T) {
	f := setup(t)
	f.addOccurrence(t, corridorWKT)

	q := query()
	// 0.05° of latitude is about 5.5 km north of the corridor, well beyond the
	// driver's 2 km notify radius.
	q.Origin = match.Point{Lon: 25.82, Lat: 40.90}

	got, err := match.Gross(context.Background(), f.pool, q)
	if err != nil {
		t.Fatalf("Gross: %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("a pickup outside the notify radius must be rejected, got %+v", got)
	}
}

func TestGrossMatchRejectsOccurrencesWithoutEnoughFreeSeats(t *testing.T) {
	f := setup(t)
	f.addOccurrence(t, corridorWKT, withSeatsFree(0))

	got, err := match.Gross(context.Background(), f.pool, query())
	if err != nil {
		t.Fatalf("Gross: %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("a full occurrence must be rejected, got %+v", got)
	}
}

func TestGrossMatchRejectsOccurrencesThatAreNotScheduled(t *testing.T) {
	for _, status := range []string{"started", "completed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			f := setup(t)
			f.addOccurrence(t, corridorWKT, withStatus(status))

			got, err := match.Gross(context.Background(), f.pool, query())
			if err != nil {
				t.Fatalf("Gross: %v", err)
			}

			if len(got) != 0 {
				t.Fatalf("a %s occurrence must be rejected, got %+v", status, got)
			}
		})
	}
}

func TestGrossMatchRejectsANonOverlappingDepartureWindow(t *testing.T) {
	f := setup(t)
	f.addOccurrence(t, corridorWKT, withWindow(
		time.Date(2026, time.July, 30, 17, 0, 0, 0, time.UTC),
		time.Date(2026, time.July, 30, 18, 0, 0, 0, time.UTC),
	))

	got, err := match.Gross(context.Background(), f.pool, query())
	if err != nil {
		t.Fatalf("Gross: %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("an evening trip must not match a morning request, got %+v", got)
	}
}

func TestGrossMatchRejectsMutedDrivers(t *testing.T) {
	f := setup(t)
	f.addOccurrence(t, corridorWKT)
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE driver_profiles SET notifications_muted = true WHERE user_id = $1`, f.driverID); err != nil {
		t.Fatalf("muting driver: %v", err)
	}

	got, err := match.Gross(context.Background(), f.pool, query())
	if err != nil {
		t.Fatalf("Gross: %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("a muted driver must not be surfaced, got %+v", got)
	}
}

// The gross filter is the only thing standing between a request and a fan-out
// of expensive OSRM calls, so it must be index-backed rather than a seq scan.
func TestGrossMatchUsesTheGeographyGISTIndex(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	// Enough dispersed routes that an index path is meaningful.
	for i := range 300 {
		lon := 25.4 + float64(i%100)*0.01
		lat := 40.4 + float64(i/100)*0.05
		f.addOccurrence(t, wktLine(lon, lat, lon+0.2, lat))
	}
	f.addOccurrence(t, corridorWKT)
	if _, err := f.pool.Exec(ctx, `ANALYZE route_templates; ANALYZE trip_occurrences; ANALYZE driver_profiles`); err != nil {
		t.Fatalf("ANALYZE: %v", err)
	}

	plan, err := match.ExplainGross(ctx, f.pool, query())
	if err != nil {
		t.Fatalf("ExplainGross: %v", err)
	}

	// Assert on the plan's structure, not on cost estimates or text formatting,
	// so the test does not break on a planner tweak.
	if !planUsesIndex(plan, "trip_occurrences_geog_gist") {
		pretty, _ := json.MarshalIndent(plan, "", "  ")
		t.Fatalf("the gross match did not use trip_occurrences_geog_gist; plan was:\n%s", pretty)
	}
}

// planUsesIndex walks an EXPLAIN (FORMAT JSON) tree looking for an index scan
// naming the given index.
func planUsesIndex(node any, indexName string) bool {
	switch n := node.(type) {
	case []any:
		for _, child := range n {
			if planUsesIndex(child, indexName) {
				return true
			}
		}
	case map[string]any:
		if name, ok := n["Index Name"].(string); ok && name == indexName {
			if nodeType, ok := n["Node Type"].(string); ok && strings.Contains(nodeType, "Index") {
				return true
			}
		}
		for _, child := range n {
			if planUsesIndex(child, indexName) {
				return true
			}
		}
	}
	return false
}

func wktLine(lon1, lat1, lon2, lat2 float64) string {
	return "LINESTRING(" +
		ftoa(lon1) + " " + ftoa(lat1) + ", " +
		ftoa(lon2) + " " + ftoa(lat2) + ")"
}

func ftoa(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

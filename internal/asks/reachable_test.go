package asks_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"carpool/internal/asks"
)

// decoy adds an occurrence on the fixture corridor and records an ask against
// it, so it counts as already offered to this request.
//
// The geometry is the fixture's own line, which gives every decoy the same
// shared distance and puts them all ahead of a slightly shorter route in the
// gross stage's ordering.
func (f *fixture) decoy(n int, requestID uuid.UUID) uuid.UUID {
	f.t.Helper()
	ctx := context.Background()

	var templateID uuid.UUID
	must(f.t, f.pool.QueryRow(ctx, `
		INSERT INTO route_templates (driver_id, vehicle_id, origin, destination, origin_label, destination_label,
			route_geom, route_km, route_min, seats, osrm_data_version)
		VALUES ($1, (SELECT id FROM vehicles WHERE owner_id = $1 AND is_default),
			ST_SetSRID(ST_MakePoint(25.80,40.85),4326), ST_SetSRID(ST_MakePoint(26.00,40.85),4326),
			$2, 'B', ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 3, 'test')
		RETURNING id`, f.driverID, fmt.Sprintf("decoy-%d", n)).Scan(&templateID), "decoy template")

	var occurrenceID uuid.UUID
	must(f.t, f.pool.QueryRow(ctx, `
		INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest, depart_latest,
			seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode)
		VALUES ($1, (SELECT vehicle_id FROM route_templates WHERE id = $1), '2026-07-30',
			'2026-07-30T04:30:00Z', '2026-07-30T05:00:00Z',
			3, 3, 1, ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 'formula')
		RETURNING id`, templateID).Scan(&occurrenceID), "decoy occurrence")

	// Declined rather than pending: a pending ask elsewhere would be a second
	// live offer, and the point here is only that this occurrence has been put
	// to the driver already.
	must(f.t, f.pool.QueryRow(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
			suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2,
			ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
			2.50, 0, 'declined', $3)
		RETURNING id`, requestID, occurrenceID, f.clock.Now().Add(asks.DefaultTTL)).Scan(new(uuid.UUID)),
		"decoy ask")

	return occurrenceID
}

// reachable adds an occurrence that genuinely matches but overlaps slightly
// less than the decoys, so the gross stage sorts it below all of them.
func (f *fixture) reachable() uuid.UUID {
	f.t.Helper()
	ctx := context.Background()

	var templateID uuid.UUID
	must(f.t, f.pool.QueryRow(ctx, `
		INSERT INTO route_templates (driver_id, vehicle_id, origin, destination, origin_label, destination_label,
			route_geom, route_km, route_min, seats, osrm_data_version)
		VALUES ($1, (SELECT id FROM vehicles WHERE owner_id = $1 AND is_default),
			ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
			'reachable', 'B', ST_GeomFromText('LINESTRING(25.82 40.85, 25.98 40.85)',4326), 13.5, 16, 3, 'test')
		RETURNING id`, f.driverID).Scan(&templateID), "reachable template")

	var occurrenceID uuid.UUID
	must(f.t, f.pool.QueryRow(ctx, `
		INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest, depart_latest,
			seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode)
		VALUES ($1, (SELECT vehicle_id FROM route_templates WHERE id = $1), '2026-07-30',
			'2026-07-30T04:30:00Z', '2026-07-30T05:00:00Z',
			3, 3, 1, ST_GeomFromText('LINESTRING(25.82 40.85, 25.98 40.85)',4326), 13.5, 16, 'formula')
		RETURNING id`, templateID).Scan(&occurrenceID), "reachable occurrence")

	return occurrenceID
}

// A driver who has never been asked must still be reachable when the drivers
// who have been asked fill the gross stage's candidate limit.
//
// The limit is applied in SQL and the already-asked filter afterwards in Go, so
// a busy corridor can hand back twenty occurrences that are all spent and hide
// every fresh one behind them. The passenger is told nobody can take them while
// a driver who would have said yes was never approached.
//
// This is the same fault the wake path would otherwise make routine: a wake
// happens precisely when a request already has ask history.
func TestAskNextReachesADriverBeyondTheCandidateLimit(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// The fixture's own ask, plus enough decoys to fill the gross stage.
	mustExec(t, f, `UPDATE asks SET status = 'declined'`)
	for n := range 19 {
		f.decoy(n, f.requestID)
	}
	want := f.reachable()
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)

	askID, err := f.matcher().AskNext(ctx, f.requestID)
	if err != nil {
		t.Fatalf("AskNext: %v, want the unasked driver to be reachable", err)
	}

	var got uuid.UUID
	must(t, f.pool.QueryRow(ctx,
		`SELECT occurrence_id FROM asks WHERE id = $1`, askID).Scan(&got), "ask occurrence")
	if got != want {
		t.Fatalf("asked about occurrence %s, want the one nobody had been asked about (%s)", got, want)
	}
}

// Nobody is asked to carry themselves.
//
// A driver who also posts a request as a passenger is an ordinary thing to do:
// the car is in the garage this week, so they look for a lift. Their own trip
// still sits on the corridor and matches on every geometric test, and without
// this the matcher offers them their own seat.
func TestAskNextDoesNotOfferAPassengerTheirOwnTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	mustExec(t, f, `DELETE FROM asks`)
	// The fixture's driver becomes the one looking for a lift.
	if _, err := f.pool.Exec(ctx,
		`UPDATE ride_requests SET passenger_id = $1, status = 'open'`, f.driverID); err != nil {
		t.Fatalf("making the driver the passenger: %v", err)
	}

	_, err := f.matcher().AskNext(ctx, f.requestID)

	if !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("AskNext: %v, want no candidates rather than the driver's own trip", err)
	}
}

package asks_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// One driver at a time, said by the schema.
//
// The rule is the heart of the matching design: a request goes to one driver,
// who has half an hour, and only when that offer ends does the next driver hear
// about it. Two live offers means two people are told a seat is theirs and at
// least one is then told it went.
//
// `asks_one_pending`, a unique index on request_id over pending rows, has
// carried it since the first migration, and nothing exercised it. That is worth
// a test rather than a shrug: an index with no test is one somebody drops while
// tidying, and the code above it — createAsk locks the ride_request and refuses
// unless it still says 'open' — would go on passing every test in the suite
// while the invariant quietly became a promise about one call site.
//
// So these go around createAsk deliberately, straight to the table. Through the
// service the lock refuses first, and the index could be gone for months.

// insertAsk writes a pending ask by hand and returns whatever the database says.
func insertAsk(t *testing.T, f *fixture, requestID, occurrenceID uuid.UUID, status string) error {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point,
		                  dropoff_point, suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		        ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, 0, $3, $4)`,
		requestID, occurrenceID, status, f.clock.Now().Add(30*time.Minute))
	return err
}

// secondOccurrence gives the fixture's driver another departure, so the second
// ask is for a different trip and the existing (request_id, occurrence_id)
// uniqueness cannot be what refuses it.
func (f *fixture) anotherDeparture(t *testing.T, daysOn int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest,
			depart_latest, seats_total, seats_free, template_revision, route_geom, route_km,
			route_min, pricing_mode)
		SELECT o.template_id, o.vehicle_id, o.service_date + $2::int,
		       o.depart_earliest + ($2::int * interval '1 day'),
		       o.depart_latest   + ($2::int * interval '1 day'),
		       3, 3, 1, o.route_geom, o.route_km, o.route_min, 'formula'
		  FROM trip_occurrences o WHERE o.id = $1
		RETURNING id`, f.occurrenceID, daysOn).Scan(&id); err != nil {
		t.Fatalf("seeding another departure: %v", err)
	}
	return id
}

func TestARequestCannotHaveTwoLiveOffersAtOnce(t *testing.T) {
	f := newFixture(t)
	// The fixture already holds one pending ask for this request.
	other := f.anotherDeparture(t, 1)

	err := insertAsk(t, f, f.requestID, other, "pending")

	if err == nil {
		t.Fatal("a second driver was given a live offer for a request that already had one")
	}
	if !isUnique(err) {
		t.Fatalf("the second offer was refused, but not by asks_one_pending: %v", err)
	}
}

// The cycle the index has to allow. A request goes to one driver, they decline,
// it goes to the next, and each spent offer stays in the table as a record of
// who was asked — which is what stops the same driver being asked twice.
func TestAskingTheNextDriverAfterADeclineIsAllowed(t *testing.T) {
	f := newFixture(t)
	other := f.anotherDeparture(t, 1)

	mustExec(t, f, `UPDATE asks SET status = 'declined', responded_at = now() WHERE id = $1`, f.askID)
	if err := insertAsk(t, f, f.requestID, other, "pending"); err != nil {
		t.Fatalf("the next driver could not be asked after a decline: %v", err)
	}

	// And again, for the one after that. Several spent offers against one
	// request is the ordinary state of a long search.
	third := f.anotherDeparture(t, 2)
	mustExec(t, f, `UPDATE asks SET status = 'expired', responded_at = now()
	                 WHERE occurrence_id = $1`, other)
	if err := insertAsk(t, f, f.requestID, third, "pending"); err != nil {
		t.Fatalf("the third driver could not be asked: %v", err)
	}
}

// The constraint by name, not merely "some uniqueness rule". There are four
// unique indexes on this table and the test is about one of them.
func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		pgErr.ConstraintName == "asks_one_pending"
}

package account_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// A subject access request is answered by what is in the file, not by what the
// person happens to remember asking for.
//
// The export covered the journeys and the conversations and stopped there. Four
// things it held about somebody were left out, and the registration plate is
// the one that makes the point: it is copied off the άδεια κυκλοφορίας, it is
// stored against their name, and the only way they could see it was to open the
// app and read the garage screen — which is not what "a copy of your data"
// means.
func TestTheExportHoldsTheCarAndTheChoicesMadeAboutOtherPeople(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	// Somebody they would rather not be matched with, and a complaint they
	// filed. Both are things they did, and both are held against their name.
	if _, err := w.pool.Exec(ctx,
		`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`,
		w.driverID, w.passengerID); err != nil {
		t.Fatalf("seeding the block: %v", err)
	}
	if _, err := w.pool.Exec(ctx,
		`INSERT INTO reports (reporter_id, reported_id, reason) VALUES ($1, $2, 'οδηγούσε επικίνδυνα')`,
		w.driverID, w.passengerID); err != nil {
		t.Fatalf("seeding the report: %v", err)
	}

	body, err := w.svc.Export(ctx, w.driverID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("the export is not readable JSON: %v", err)
	}

	for _, section := range []string{"vehicles", "driver_settings", "people_blocked", "reports_made"} {
		items, ok := out[section].([]any)
		if !ok || len(items) == 0 {
			t.Errorf("%s is missing from the export", section)
		}
	}

	cars, _ := out["vehicles"].([]any)
	if len(cars) > 0 {
		car, _ := cars[0].(map[string]any)
		if car["plate"] == nil || car["plate"] == "" {
			t.Errorf("the export lists a car without its plate: %v", car)
		}
	}
}

// A report names somebody else. What the person filed is theirs to have; who
// they filed it about is a second person's business, and a report filed against
// them is not theirs at all — it would tell them who complained.
func TestTheExportDoesNotHandOverTheOtherPersonInAComplaint(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if _, err := w.pool.Exec(ctx,
		`INSERT INTO reports (reporter_id, reported_id, reason) VALUES ($1, $2, 'με απείλησε')`,
		w.driverID, w.passengerID); err != nil {
		t.Fatalf("seeding the report: %v", err)
	}

	body, err := w.svc.Export(ctx, w.passengerID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("the export is not readable JSON: %v", err)
	}
	if items, _ := out["reports_made"].([]any); len(items) != 0 {
		t.Fatalf("somebody's export lists a complaint made about them: %v", items)
	}
	if strings.Contains(string(body), w.driverID.String()) {
		t.Fatal("the export carries another person's id")
	}
}

// Erasure has to reach the garage.
//
// The driver_profiles row is scrubbed, and there is a careful comment saying
// why the row survives the scrubbing. The vehicles table arrived later and was
// never added: a registration plate, a make, a model and a colour stayed
// readable against an account whose owner had asked to be erased. A plate is
// how somebody is found — it is the one identifier here that a stranger can
// look up while standing next to the car.
func TestErasureReachesTheGarage(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if err := w.svc.Delete(ctx, w.driverID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	rows, err := w.pool.Query(ctx,
		`SELECT plate, make, model, colour FROM vehicles WHERE owner_id = $1`, w.driverID)
	if err != nil {
		t.Fatalf("reading the garage: %v", err)
	}
	defer rows.Close()

	var seen int
	for rows.Next() {
		var plate, make_ string
		var model string
		var colour *string
		if err := rows.Scan(&plate, &make_, &model, &colour); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		seen++
		if plate != "—" {
			t.Errorf("the plate %q survives erasure", plate)
		}
		if colour != nil {
			t.Errorf("the colour %q survives erasure", *colour)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the garage: %v", err)
	}
	if seen == 0 {
		t.Fatal("the fixture has no car, so this test proves nothing")
	}
}

// Closing an account withdraws the offers sitting with the person, and the
// passenger on the other end of one must not be left waiting on an answer that
// will never come. Same reasoning as the suspension cascade: a request at
// 'asking' with no live offer is a request nothing looks at again.
func TestClosingAnAccountPutsItsWaitingPassengersBack(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	var requestID uuid.UUID
	if err := w.pool.QueryRow(ctx, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end,
		                           seats, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		        now(), now() + interval '2 hours', 1, 'open', 'asking')
		RETURNING id`, w.passengerID).Scan(&requestID); err != nil {
		t.Fatalf("seeding the request: %v", err)
	}
	if _, err := w.pool.Exec(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
		                  suggested_price_eur, itinerary_version, status, expires_at)
		SELECT $1, o.id, 5, 2, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		       ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, 0, 'pending', now() + interval '1 hour'
		  FROM trip_occurrences o
		  JOIN route_templates t ON t.id = o.template_id
		 WHERE t.driver_id = $2
		 LIMIT 1`, requestID, w.driverID); err != nil {
		t.Fatalf("seeding the ask: %v", err)
	}

	// The driver leaves, so the offer they were holding goes with them.
	if err := w.svc.Delete(ctx, w.driverID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var status string
	if err := w.pool.QueryRow(ctx,
		`SELECT status FROM ride_requests WHERE id = $1`, requestID).Scan(&status); err != nil {
		t.Fatalf("reading the request: %v", err)
	}
	if status != "open" {
		t.Fatalf("the waiting passenger's request is %q; nothing looks at one in that state again", status)
	}
}

// And they are asked again straight away rather than waiting for the hourly
// reconciler. A passenger whose driver closed their account should not spend up
// to an hour on the searching screen to be told somebody else can take them.
func TestClosingAnAccountAsksTheNextDriverForThosePassengers(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	var requestID uuid.UUID
	if err := w.pool.QueryRow(ctx, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end,
		                           seats, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		        now(), now() + interval '2 hours', 1, 'open', 'asking')
		RETURNING id`, w.passengerID).Scan(&requestID); err != nil {
		t.Fatalf("seeding the request: %v", err)
	}
	if _, err := w.pool.Exec(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
		                  suggested_price_eur, itinerary_version, status, expires_at)
		SELECT $1, o.id, 5, 2, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		       ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, 0, 'pending', now() + interval '1 hour'
		  FROM trip_occurrences o
		  JOIN route_templates t ON t.id = o.template_id
		 WHERE t.driver_id = $2
		 LIMIT 1`, requestID, w.driverID); err != nil {
		t.Fatalf("seeding the ask: %v", err)
	}

	if err := w.svc.Delete(ctx, w.driverID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var queued int
	if err := w.pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox WHERE kind = 'ask_next'`).Scan(&queued); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if queued == 0 {
		t.Fatal("no next driver is queued: the passenger waits for the hourly sweep at best")
	}
}

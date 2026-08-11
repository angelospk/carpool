package moderation_test

import (
	"context"
	"testing"
	"time"

	"carpool/internal/jobs"
)

// A suspension has to survive the next scheduled job, and this one did not.
//
// The cascade cancels every departure that has not left. It does not touch the
// route_templates behind them, and the materializer's whole job is to make the
// occurrences a template's schedule implies — including reviving one that was
// cancelled, which is deliberate and right when a driver lifts an exception
// they had set. It reads the template's `active` flag and nothing about the
// person driving.
//
// So the cascade cancels the trips, the bookings in them go with them, and the
// next materializer pass finds occurrences with no confirmed booking and no
// pending ask, which is exactly its definition of one it may touch. It sets them
// back to 'scheduled'. A suspended driver is in the matching pool again within
// one tick, and the passengers matched to them are matched to somebody who
// cannot open the app to agree a meeting point or say they are outside.
//
// Deleting an account already gets this right: it sets active = false on the
// templates, so the materializer skips them entirely. Suspension simply forgot.
func TestSuspensionSurvivesTheMaterializer(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)

	// A schedule, so the materializer has something to plan. Wednesday is the
	// weekday of the fixture's own tomorrow, 30 July 2026.
	if _, err := w.pool.Exec(ctx, `
		INSERT INTO route_schedules (template_id, kind, weekdays, depart_earliest, depart_latest)
		SELECT t.id, 'recurring', ARRAY[1,2,3,4,5,6,7], '07:30', '08:00'
		  FROM route_templates t WHERE t.driver_id = $1`,
		w.driver); err != nil {
		t.Fatalf("seeding the schedule: %v", err)
	}

	w.suspend()
	if got := w.occurrenceStatus(w.tomorrow); got != "cancelled" {
		t.Fatalf("tomorrow is %q straight after the suspension, want cancelled", got)
	}

	if err := jobs.NewMaterializer(w.pool, w.clock, athens(t)).Run(ctx); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if got := w.occurrenceStatus(w.tomorrow); got != "cancelled" {
		t.Fatalf("the materializer put a suspended driver's trip back to %q", got)
	}

	// And it must not plan new ones either, which is the same bug a day later.
	var scheduled int
	if err := w.pool.QueryRow(ctx, `
		SELECT count(*) FROM trip_occurrences o
		JOIN route_templates t ON t.id = o.template_id
		WHERE t.driver_id = $1 AND o.status = 'scheduled'
		  AND o.depart_earliest > $2`, w.driver, w.clock.Now()).Scan(&scheduled); err != nil {
		t.Fatalf("counting scheduled departures: %v", err)
	}
	if scheduled != 0 {
		t.Fatalf("%d departures are scheduled for a suspended driver, want none", scheduled)
	}
}

// Reinstatement gives back the ability to drive. It does not give back the
// particular mornings that were called off — those passengers were told the trip
// was cancelled and have made other arrangements — but the route itself must
// work again, or a suspension anybody comes back from is really a deletion.
func TestReinstatementLetsTheMaterializerPlanAgain(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)

	if _, err := w.pool.Exec(ctx, `
		INSERT INTO route_schedules (template_id, kind, weekdays, depart_earliest, depart_latest)
		SELECT t.id, 'recurring', ARRAY[1,2,3,4,5,6,7], '07:30', '08:00'
		  FROM route_templates t WHERE t.driver_id = $1`,
		w.driver); err != nil {
		t.Fatalf("seeding the schedule: %v", err)
	}

	w.suspend()
	if err := w.svc.Reinstate(ctx, w.driver, w.moderator, "appealed"); err != nil {
		t.Fatalf("Reinstate: %v", err)
	}

	// A week on, so the horizon has moved past the mornings that were called
	// off and the only departures in scope are new ones.
	w.clock.Advance(7 * 24 * time.Hour)
	if err := jobs.NewMaterializer(w.pool, w.clock, athens(t)).Run(ctx); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	var scheduled int
	if err := w.pool.QueryRow(ctx, `
		SELECT count(*) FROM trip_occurrences o
		JOIN route_templates t ON t.id = o.template_id
		WHERE t.driver_id = $1 AND o.status = 'scheduled'
		  AND o.depart_earliest > $2`, w.driver, w.clock.Now()).Scan(&scheduled); err != nil {
		t.Fatalf("counting scheduled departures: %v", err)
	}
	if scheduled == 0 {
		t.Fatal("a reinstated driver's schedule plans nothing: the suspension was permanent after all")
	}
}

func athens(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Athens")
	if err != nil {
		return time.UTC
	}
	return loc
}

package drives_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/drives"
	"carpool/internal/notices"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

// The departure the schedule gives: 07:00 to 07:30 on a twenty-minute route
// along the fixture corridor.
var (
	windowOpens  = time.Date(2026, time.August, 4, 7, 0, 0, 0, time.UTC)
	windowCloses = windowOpens.Add(30 * time.Minute)
)

type world struct {
	pool       *pgxpool.Pool
	clock      *clock.Fake
	svc        *drives.Service
	regionID   uuid.UUID
	driverID   uuid.UUID
	templateID uuid.UUID
	occ        uuid.UUID
}

func newWorld(t *testing.T, now time.Time) *world {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	w := &world{pool: pool, clock: clock.NewFake(now)}
	w.svc = drives.NewService(pool, w.clock)

	w.scan(t, &w.regionID, `INSERT INTO regions (name, boundary)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326)))
		RETURNING id`)
	w.driverID = w.user(t, "+306900000001")
	w.exec(t, `INSERT INTO driver_profiles (user_id, car_model, car_plate, notify_radius_km)
		VALUES ($1, 'Yaris', 'ΑΒΓ-1234', 2.0)`, w.driverID)
	testdb.GiveEveryDriverACar(t, pool)
	w.scan(t, &w.templateID, `INSERT INTO route_templates (driver_id, vehicle_id, origin, destination,
		origin_label, destination_label, route_geom, route_km, route_min, seats, osrm_data_version,
		nudge_when_leaving)
		VALUES ($1, (SELECT id FROM vehicles WHERE owner_id = $1 AND is_default),
		ST_SetSRID(ST_MakePoint(25.80,40.85),4326), ST_SetSRID(ST_MakePoint(26.00,40.85),4326),
		'A','B', ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 3, 'test', true)
		RETURNING id`, w.driverID)
	w.scan(t, &w.occ, `INSERT INTO trip_occurrences (template_id, vehicle_id, service_date,
		depart_earliest, depart_latest, seats_total, seats_free, template_revision, route_geom,
		route_km, route_min, pricing_mode)
		VALUES ($1, (SELECT vehicle_id FROM route_templates WHERE id = $1), $2::date, $3, $4, 3, 3, 1,
		ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 'formula')
		RETURNING id`, w.templateID, windowOpens.Format("2006-01-02"), windowOpens, windowCloses)
	return w
}

func (w *world) scan(t *testing.T, dest *uuid.UUID, sql string, args ...any) {
	t.Helper()
	if err := w.pool.QueryRow(context.Background(), sql, args...).Scan(dest); err != nil {
		t.Fatalf("seeding: %v", err)
	}
}

func (w *world) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := w.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("seeding: %v", err)
	}
}

func (w *world) user(t *testing.T, phone string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	w.scan(t, &id, `INSERT INTO users (date_of_birth, phone, name, home_region_id)
		VALUES (DATE '1990-01-01', $1, 'Someone', $2) RETURNING id`, phone, w.regionID)
	return id
}

// request is somebody at the western end of the corridor, about 110 m off the
// road, going to the eastern end. The car reaches them two minutes after it
// leaves.
func (w *world) request(t *testing.T, phone string, readyFrom, until time.Time) (passenger, request uuid.UUID) {
	t.Helper()
	passenger = w.user(t, phone)
	w.scan(t, &request, `INSERT INTO ride_requests (passenger_id, origin, destination,
		window_start, window_end, seats, kind, status, created_at)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.851),4326), ST_SetSRID(ST_MakePoint(25.98,40.851),4326),
		$2, $3, 1, 'open', 'open', $4) RETURNING id`, passenger, readyFrom, until, readyFrom)
	return passenger, request
}

func (w *world) ask(t *testing.T, request uuid.UUID, expires time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	w.exec(t, `UPDATE ride_requests SET status = 'asking' WHERE id = $1`, request)
	w.scan(t, &id, `INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km,
		pickup_point, dropoff_point, suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 1, 0.5, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, 0, 'pending', $3) RETURNING id`,
		request, w.occ, expires)
	return id
}

func (w *world) book(t *testing.T, phone string) (passenger, booking uuid.UUID) {
	t.Helper()
	passenger, request := w.request(t, phone, windowOpens.Add(-time.Hour), windowCloses)
	askID := w.ask(t, request, windowOpens)
	w.exec(t, `UPDATE asks SET status = 'accepted' WHERE id = $1`, askID)
	w.exec(t, `UPDATE ride_requests SET status = 'matched' WHERE id = $1`, request)
	w.scan(t, &booking, `INSERT INTO bookings (occurrence_id, request_id, passenger_id, ask_id, seats,
		price_eur, pickup_point, dropoff_point)
		VALUES ($1, $2, $3, $4, 1, 2.50, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		ST_SetSRID(ST_MakePoint(25.98,40.85),4326)) RETURNING id`, w.occ, request, passenger, askID)
	w.exec(t, `UPDATE trip_occurrences SET seats_free = seats_free - 1 WHERE id = $1`, w.occ)
	return passenger, booking
}

type occurrence struct {
	earliest, latest       time.Time
	scheduledE, scheduledL *time.Time
	driveSetAt             *time.Time
	detourMin              int
	allowStops             bool
}

func (w *world) occurrence(t *testing.T) occurrence {
	t.Helper()
	var o occurrence
	if err := w.pool.QueryRow(context.Background(), `
		SELECT depart_earliest, depart_latest, scheduled_earliest, scheduled_latest, drive_set_at,
		       detour_min, allow_stops
		  FROM trip_occurrences WHERE id = $1`, w.occ).Scan(
		&o.earliest, &o.latest, &o.scheduledE, &o.scheduledL, &o.driveSetAt, &o.detourMin, &o.allowStops); err != nil {
		t.Fatalf("reading the occurrence: %v", err)
	}
	return o
}

func (w *world) outbox(t *testing.T, kind string) []json.RawMessage {
	t.Helper()
	rows, err := w.pool.Query(context.Background(),
		`SELECT payload FROM outbox WHERE kind = $1 ORDER BY id`, kind)
	if err != nil {
		t.Fatalf("reading the outbox: %v", err)
	}
	defer rows.Close()
	var out []json.RawMessage
	for rows.Next() {
		var p json.RawMessage
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scanning the outbox: %v", err)
		}
		out = append(out, p)
	}
	return out
}

func (w *world) notices(t *testing.T, event notices.Event) []notices.Payload {
	t.Helper()
	var out []notices.Payload
	for _, raw := range w.outbox(t, "push") {
		var p notices.Payload
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("decoding a notice: %v", err)
		}
		if p.Event == event {
			out = append(out, p)
		}
	}
	return out
}

func at(hhmm string) time.Time {
	tm, err := time.Parse("15:04", hhmm)
	if err != nil {
		panic(err)
	}
	return time.Date(2026, time.August, 4, tm.Hour(), tm.Minute(), 0, 0, time.UTC)
}

func start(w *world, departs time.Time) (time.Time, error) {
	return w.svc.Start(context.Background(), w.driverID, w.occ, drives.Start{DepartsAt: departs})
}

// ---------------------------------------------------------------- Start ---

func TestStartingADrivePinsTheDepartureToThatMinute(t *testing.T) {
	w := newWorld(t, at("06:55"))

	got, err := start(w, at("07:05").Add(20*time.Second))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !got.Equal(at("07:05")) {
		t.Errorf("departs %v, want 07:05 on the minute", got)
	}
	o := w.occurrence(t)
	if !o.earliest.Equal(at("07:05")) || !o.latest.Equal(at("07:05")) {
		t.Errorf("window is %v–%v, want 07:05–07:05", o.earliest, o.latest)
	}
	if o.scheduledE == nil || !o.scheduledE.Equal(windowOpens) || o.scheduledL == nil || !o.scheduledL.Equal(windowCloses) {
		t.Errorf("the published window was not kept: %v–%v", o.scheduledE, o.scheduledL)
	}
	if o.driveSetAt == nil || !o.driveSetAt.Equal(at("06:55")) {
		t.Errorf("drive_set_at = %v", o.driveSetAt)
	}
	// A trip with a real time is a trip somebody waiting may now fit.
	if n := len(w.outbox(t, "wake_waiting")); n != 1 {
		t.Errorf("queued %d wakes, want 1", n)
	}
}

// The second change of mind is measured against what was published, not
// against the first: otherwise "07:05" then "07:20" would be refused as
// outside a window that is now one minute wide.
func TestChangingTheMinuteIsMeasuredAgainstTheSchedule(t *testing.T) {
	w := newWorld(t, at("06:55"))
	if _, err := start(w, at("07:05")); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	w.clock.Set(at("07:00"))
	if _, err := start(w, at("07:12")); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	o := w.occurrence(t)
	if !o.earliest.Equal(at("07:12")) || !o.scheduledE.Equal(windowOpens) || !o.scheduledL.Equal(windowCloses) {
		t.Errorf("after two changes: %v, scheduled %v–%v", o.earliest, o.scheduledE, o.scheduledL)
	}
}

// A request retried by a phone on a bad connection is one change: one wake and
// one word to each passenger.
func TestARetriedStartIsOneChange(t *testing.T) {
	w := newWorld(t, at("06:55"))
	_, booking := w.book(t, "+306900000010")

	for i := 0; i < 2; i++ {
		if _, err := start(w, at("07:05")); err != nil {
			t.Fatalf("Start #%d: %v", i+1, err)
		}
		w.clock.Advance(40 * time.Second)
	}
	if n := len(w.outbox(t, "wake_waiting")); n != 1 {
		t.Errorf("queued %d wakes, want 1", n)
	}
	got := w.notices(t, notices.DriveRetimed)
	if len(got) != 1 || *got[0].BookingID != booking {
		t.Errorf("told the passenger %d times: %+v", len(got), got)
	}
}

// The late driver from RISKS.md: their window has opened, they have not left,
// nobody has a seat. Saying when they are really going is exactly the point.
func TestALateDriverCanStillSayWhenTheyAreLeaving(t *testing.T) {
	w := newWorld(t, at("07:25"))
	if _, err := start(w, at("07:35")); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if o := w.occurrence(t); !o.earliest.Equal(at("07:35")) {
		t.Errorf("departs %v", o.earliest)
	}
}

func TestADepartureMustBeWithinTheNextQuarterOfAnHour(t *testing.T) {
	w := newWorld(t, at("06:50"))
	for _, departs := range []time.Time{at("07:06"), at("06:48")} {
		if _, err := start(w, departs); !errors.Is(err, drives.ErrBadTime) {
			t.Errorf("departs %s: err = %v, want ErrBadTime", departs.Format("15:04"), err)
		}
	}
	if _, err := start(w, at("07:05")); err != nil {
		t.Errorf("exactly fifteen minutes ahead: %v", err)
	}
}

// Booked passengers agreed to the published window. The driver can narrow it,
// and run a little late, but not leave before it opens.
func TestBookedPassengersHoldTheDriverToTheirWindow(t *testing.T) {
	w := newWorld(t, at("06:50"))
	w.book(t, "+306900000010")
	if _, err := start(w, at("06:55")); !errors.Is(err, drives.ErrOutsideWindow) {
		t.Errorf("leaving before the booked window: err = %v, want ErrOutsideWindow", err)
	}

	w.clock.Set(at("07:40"))
	if _, err := start(w, at("07:45")); err != nil {
		t.Errorf("a quarter of an hour late with passengers: %v", err)
	}
	w.clock.Set(at("07:41"))
	if _, err := start(w, at("07:46")); !errors.Is(err, drives.ErrOutsideWindow) {
		t.Errorf("more than a quarter of an hour late with passengers: err = %v", err)
	}
}

func TestAnEmptyCarCanMoveAnHourEitherWay(t *testing.T) {
	w := newWorld(t, at("05:55"))
	if _, err := start(w, at("06:00")); err != nil {
		t.Errorf("an hour early with nobody booked: %v", err)
	}
	w2 := newWorld(t, at("05:50"))
	if _, err := start(w2, at("05:59")); !errors.Is(err, drives.ErrOutsideWindow) {
		t.Errorf("more than an hour early: err = %v", err)
	}
}

func TestOnlyTheDriverCanStartTheirDrive(t *testing.T) {
	w := newWorld(t, at("06:55"))
	stranger := w.user(t, "+306900000099")
	_, err := w.svc.Start(context.Background(), stranger, w.occ, drives.Start{DepartsAt: at("07:05")})
	if !errors.Is(err, drives.ErrNotYours) {
		t.Errorf("a stranger: err = %v", err)
	}
	_, err = w.svc.Start(context.Background(), w.driverID, uuid.New(), drives.Start{DepartsAt: at("07:05")})
	if !errors.Is(err, drives.ErrNotYours) {
		t.Errorf("no such trip: err = %v", err)
	}
	w.exec(t, `UPDATE users SET status = 'suspended' WHERE id = $1`, w.driverID)
	if _, err := start(w, at("07:05")); !errors.Is(err, drives.ErrNotYours) {
		t.Errorf("a suspended driver: err = %v", err)
	}
}

func TestACancelledTripCannotBeDriven(t *testing.T) {
	w := newWorld(t, at("06:55"))
	w.exec(t, `UPDATE trip_occurrences SET status = 'cancelled' WHERE id = $1`, w.occ)
	if _, err := start(w, at("07:05")); !errors.Is(err, drives.ErrNotScheduled) {
		t.Errorf("err = %v, want ErrNotScheduled", err)
	}
}

// Once the pinned minute has passed the car has gone. Moving it again is the
// moving drive of phase 2, not this.
func TestADriveThatHasLeftCannotBeRetimed(t *testing.T) {
	w := newWorld(t, at("06:55"))
	if _, err := start(w, at("07:00")); err != nil {
		t.Fatalf("Start: %v", err)
	}
	w.clock.Set(at("07:01"))
	if _, err := start(w, at("07:10")); !errors.Is(err, drives.ErrDeparted) {
		t.Errorf("err = %v, want ErrDeparted", err)
	}
}

// Offers already on the driver's phone were made against the old window. The
// ones the new minute rules out are let go now, so the passenger is moved on
// to somebody else instead of waiting half an hour for an answer that can no
// longer be yes; the others end at the last moment the passenger could be
// told and still be at the road first, like every new offer.
func TestOffersTheNewMinuteRulesOutAreLetGo(t *testing.T) {
	w := newWorld(t, at("06:50"))
	_, late := w.request(t, "+306900000020", at("07:20"), at("09:00"))  // ready after the car passes
	_, ready := w.request(t, "+306900000021", at("06:30"), at("09:00")) // ready now
	lateAsk := w.ask(t, late, at("07:15"))
	readyAsk := w.ask(t, ready, at("07:15"))

	if _, err := start(w, at("07:00")); err != nil {
		t.Fatalf("Start: %v", err)
	}
	expiry := func(id uuid.UUID) time.Time {
		var e time.Time
		if err := w.pool.QueryRow(context.Background(),
			`SELECT expires_at FROM asks WHERE id = $1`, id).Scan(&e); err != nil {
			t.Fatalf("reading the ask: %v", err)
		}
		return e
	}
	if got := expiry(lateAsk); !got.Equal(at("06:50")) {
		t.Errorf("an offer the passenger cannot make expires at %v, want now", got)
	}
	// The car passes about 07:02; three minutes' margin and a minute and a
	// half to walk 110 m make the last moment to tell them about 06:57:38.
	if got := expiry(readyAsk); got.Sub(at("06:57").Add(38*time.Second)).Abs() > 2*time.Second {
		t.Errorf("an offer that still works expires at %v, want the last moment they can be told", got)
	}
}

// A new budget is a different promise, and an offer quoted under the old one
// is let go rather than accepted under terms the driver just took back.
func TestChangingTheDetourLetsGoOfOffersMadeUnderTheOldOne(t *testing.T) {
	w := newWorld(t, at("06:50"))
	_, ready := w.request(t, "+306900000021", at("06:30"), at("09:00"))
	askID := w.ask(t, ready, at("07:15"))

	none := 0
	if _, err := w.svc.Start(context.Background(), w.driverID, w.occ,
		drives.Start{DepartsAt: at("07:00"), DetourMin: &none}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	o := w.occurrence(t)
	// "My road only" still stops for somebody on it: a minute to pull over,
	// and stops allowed.
	if o.detourMin != 1 || !o.allowStops {
		t.Errorf("detour %d, stops %v; want a minute and stops allowed", o.detourMin, o.allowStops)
	}
	var e time.Time
	if err := w.pool.QueryRow(context.Background(),
		`SELECT expires_at FROM asks WHERE id = $1`, askID).Scan(&e); err != nil {
		t.Fatalf("reading the ask: %v", err)
	}
	if !e.Equal(at("06:50")) {
		t.Errorf("an offer made under the old budget expires at %v, want now", e)
	}

	tooMuch := 31
	if _, err := w.svc.Start(context.Background(), w.driverID, w.occ,
		drives.Start{DepartsAt: at("07:00"), DetourMin: &tooMuch}); !errors.Is(err, drives.ErrBadDetour) {
		t.Errorf("a 31-minute detour: err = %v", err)
	}
}

// ---------------------------------------------------------------- Nudge ---

func nudgeWorld(t *testing.T) *world {
	w := newWorld(t, at("06:50"))
	w.request(t, "+306900000030", at("06:40"), at("09:00"))
	return w
}

func (w *world) nudge(t *testing.T) int {
	t.Helper()
	n, err := w.svc.Nudge(context.Background())
	if err != nil {
		t.Fatalf("Nudge: %v", err)
	}
	return n
}

func TestADriverIsToldOnceThatSomebodyOnTheirRoadIsWaiting(t *testing.T) {
	w := nudgeWorld(t)
	w.request(t, "+306900000031", at("06:40"), at("09:00"))

	if n := w.nudge(t); n != 1 {
		t.Fatalf("nudged %d departures, want 1", n)
	}
	got := w.notices(t, notices.DriveNudge)
	if len(got) != 1 || got[0].RecipientID != w.driverID || *got[0].OccurrenceID != w.occ || got[0].Count != 2 {
		t.Fatalf("notices: %+v", got)
	}
	w.clock.Advance(time.Minute)
	if n := w.nudge(t); n != 0 || len(w.notices(t, notices.DriveNudge)) != 1 {
		t.Fatalf("a second run nudged again")
	}
}

func TestNobodyIsNudgedWhenThereIsNothingToSay(t *testing.T) {
	cases := map[string]func(t *testing.T, w *world){
		"the driver did not ask": func(t *testing.T, w *world) {
			w.exec(t, `UPDATE route_templates SET nudge_when_leaving = false WHERE id = $1`, w.templateID)
		},
		"the car is full": func(t *testing.T, w *world) {
			w.exec(t, `UPDATE trip_occurrences SET seats_free = 0 WHERE id = $1`, w.occ)
		},
		"they already said when they leave": func(t *testing.T, w *world) {
			if _, err := start(w, at("07:00")); err != nil {
				t.Fatalf("Start: %v", err)
			}
		},
		"notifications are muted": func(t *testing.T, w *world) {
			w.exec(t, `UPDATE driver_profiles SET notifications_muted = true WHERE user_id = $1`, w.driverID)
		},
		"the departure is still far off": func(t *testing.T, w *world) {
			w.clock.Set(at("06:40"))
		},
		"nobody is ready in time": func(t *testing.T, w *world) {
			w.exec(t, `UPDATE ride_requests SET window_start = $1`, at("07:20"))
		},
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			w := nudgeWorld(t)
			arrange(t, w)
			if n := w.nudge(t); n != 0 {
				t.Errorf("nudged %d", n)
			}
			if got := w.notices(t, notices.DriveNudge); len(got) != 0 {
				t.Errorf("queued %+v", got)
			}
		})
	}
}

// Two workers running the same minute send one nudge.
func TestConcurrentNudgesSendOne(t *testing.T) {
	w := nudgeWorld(t)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() {
			_, err := w.svc.Nudge(context.Background())
			errs <- err
		}()
	}
	for i := 0; i < 4; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("Nudge: %v", err)
		}
	}
	if got := w.notices(t, notices.DriveNudge); len(got) != 1 {
		t.Fatalf("queued %d nudges, want 1", len(got))
	}
}

// A passenger is told when the car reaches their own pickup, a tenth of the
// way along a twenty-minute road: two minutes after it leaves.
func TestAPassengerSeesWhenTheCarReachesThem(t *testing.T) {
	w := newWorld(t, at("06:55"))
	passenger, _ := w.book(t, "+306900000010")
	if _, err := start(w, at("07:05")); err != nil {
		t.Fatalf("Start: %v", err)
	}
	trip, err := store.GetTrip(context.Background(), w.pool, w.occ, passenger)
	if err != nil {
		t.Fatalf("GetTrip: %v", err)
	}
	if trip.PickupAt == nil || trip.PickupAt.Sub(at("07:07")).Abs() > 5*time.Second {
		t.Fatalf("pickup at %v, want about 07:07", trip.PickupAt)
	}
	driver, err := store.GetTrip(context.Background(), w.pool, w.occ, w.driverID)
	if err != nil {
		t.Fatalf("GetTrip as the driver: %v", err)
	}
	if driver.PickupAt != nil {
		t.Errorf("the driver was given a pickup time: %v", driver.PickupAt)
	}
}

// Saying when you leave without touching the detour leaves the detour alone.
func TestStartingWithoutADetourKeepsTheOneTheTripHad(t *testing.T) {
	w := newWorld(t, at("06:55"))
	w.exec(t, `UPDATE trip_occurrences SET detour_min = 8 WHERE id = $1`, w.occ)
	if _, err := start(w, at("07:00")); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if o := w.occurrence(t); o.detourMin != 8 {
		t.Fatalf("detour became %d, want the 8 it was", o.detourMin)
	}
}

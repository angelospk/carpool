package match_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/match"
)

// A drive is a departure the driver has pinned to one minute, so the matcher
// knows when the car reaches each pickup rather than only when it might leave.
//
// The fixture's corridor is twenty minutes long and the default request's
// origin sits a tenth of the way along it, about 110 m off the road. A car
// leaving at 07:00 is there at about 07:02; the passenger needs three minutes'
// margin plus a minute and a half to walk, so they must be ready by about
// 06:57:30 and still waiting at 07:02.

var driveDeparts = time.Date(2026, time.July, 30, 7, 0, 0, 0, time.UTC)

func (f *fixture) pinDrive(t *testing.T, occurrenceID uuid.UUID, departs time.Time) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `
		UPDATE trip_occurrences
		   SET scheduled_earliest = depart_earliest, scheduled_latest = depart_latest,
		       depart_earliest = $2::timestamptz, depart_latest = $2::timestamptz, drive_set_at = $2::timestamptz - interval '10 minutes',
		       offers_until = $2::timestamptz + route_min * interval '1 minute'
		 WHERE id = $1`, occurrenceID, departs); err != nil {
		t.Fatalf("pinning the drive: %v", err)
	}
}

func driveQuery(start, end time.Time) match.GrossQuery {
	q := query()
	q.WindowStart, q.WindowEnd = start, end
	q.Now = driveDeparts.Add(-10 * time.Minute)
	return q
}

func driveGross(t *testing.T, f *fixture, q match.GrossQuery) []match.Candidate {
	t.Helper()
	got, err := match.Gross(context.Background(), f.pool, q)
	if err != nil {
		t.Fatalf("Gross: %v", err)
	}
	return got
}

func TestADriveIsOfferedToSomebodyReadyBeforeTheCarComes(t *testing.T) {
	f := setup(t)
	occ := f.addOccurrence(t, corridorWKT)
	f.pinDrive(t, occ, driveDeparts)

	got := driveGross(t, f, driveQuery(driveDeparts.Add(-30*time.Minute), driveDeparts.Add(2*time.Hour)))
	if len(got) != 1 || got[0].OccurrenceID != occ {
		t.Fatalf("a passenger ready and waiting was not offered the drive: %+v", got)
	}
}

// Ready at 06:59, which is before the car leaves but after it would need
// them to set off for the road. The driver would wait. Same request against the same trip without a pinned
// departure still matches, so the refusal is the drive's doing.
func TestADriveIsNotOfferedToSomebodyNotReadyWhenTheCarPasses(t *testing.T) {
	f := setup(t)
	occ := f.addOccurrence(t, corridorWKT)
	q := driveQuery(driveDeparts.Add(-time.Minute), driveDeparts.Add(2*time.Hour))

	if got := driveGross(t, f, q); len(got) != 1 {
		t.Fatalf("control: the unpinned trip should match a window opening at its departure: %+v", got)
	}

	f.pinDrive(t, occ, driveDeparts)
	if got := driveGross(t, f, q); len(got) != 0 {
		t.Fatalf("a passenger only ready as the car passes was offered the drive: %+v", got)
	}
}

func TestADriveIsNotOfferedToSomebodyWhoStopsWaitingBeforeTheCarComes(t *testing.T) {
	f := setup(t)
	occ := f.addOccurrence(t, corridorWKT)
	f.pinDrive(t, occ, driveDeparts)

	got := driveGross(t, f, driveQuery(driveDeparts.Add(-30*time.Minute), driveDeparts.Add(time.Minute)))
	if len(got) != 0 {
		t.Fatalf("a passenger who gives up at 07:01 was offered a car arriving at 07:02: %+v", got)
	}
}

// Walking is part of being ready. A passenger 600 m from the road needs about
// seven and a half minutes to reach it, so a window opening at 06:52 is not
// early enough for a car passing at 07:02 even though 06:52 is well before it.
func TestADriveCountsTheWalkToTheRoad(t *testing.T) {
	f := setup(t)
	occ := f.addOccurrence(t, corridorWKT)
	f.pinDrive(t, occ, driveDeparts)

	q := driveQuery(driveDeparts.Add(-8*time.Minute), driveDeparts.Add(2*time.Hour))
	q.Origin = match.Point{Lon: 25.82, Lat: 40.8554} // ~600 m north of the road
	if got := driveGross(t, f, q); len(got) != 0 {
		t.Fatalf("the walk to the road was not counted: %+v", got)
	}

	q.Origin = query().Origin // ~110 m: ready in time
	if got := driveGross(t, f, q); len(got) != 1 {
		t.Fatalf("control: a passenger next to the road should be offered it: %+v", got)
	}
}

// Further down the road the car comes later, and a window that opens after the
// car has left its origin is still a match. The dropoff end of the corridor is
// reached at about 07:18; a passenger at 07:10 a few km before it is fine.
func TestADriveReachesPassengersWhoseWindowOpensAfterItLeaves(t *testing.T) {
	f := setup(t)
	occ := f.addOccurrence(t, corridorWKT)
	f.pinDrive(t, occ, driveDeparts)

	q := driveQuery(driveDeparts.Add(5*time.Minute), driveDeparts.Add(2*time.Hour))
	q.Origin = match.Point{Lon: 25.90, Lat: 40.851} // halfway: the car passes about 07:10
	q.RequestKm = 6
	got := driveGross(t, f, q)
	if len(got) != 1 || got[0].OccurrenceID != occ {
		t.Fatalf("a passenger down the road, ready at 07:05 for a car passing at 07:10, was not offered it: %+v", got)
	}
}

// Ready an hour ago is not the same as standing at the road now. Somebody
// 600 m away, told at 06:55 about a car passing at 07:02, cannot walk there in
// time, however early their window opened.
func TestADriveCountsFromNowNotFromWhenTheWindowOpened(t *testing.T) {
	f := setup(t)
	occ := f.addOccurrence(t, corridorWKT)
	f.pinDrive(t, occ, driveDeparts)

	q := driveQuery(driveDeparts.Add(-time.Hour), driveDeparts.Add(2*time.Hour))
	q.Origin = match.Point{Lon: 25.82, Lat: 40.8554}
	q.Now = driveDeparts.Add(-5 * time.Minute)
	if got := driveGross(t, f, q); len(got) != 0 {
		t.Fatalf("a passenger with a seven-minute walk was offered a car seven minutes away: %+v", got)
	}

	q.Now = driveDeparts.Add(-15 * time.Minute)
	if got := driveGross(t, f, q); len(got) != 1 {
		t.Fatalf("control: with a quarter of an hour in hand the walk fits: %+v", got)
	}
}

// The containment contract from MATCHING.md: whatever the forward filter
// would offer a drive, waking the drive must reach.
func TestWaitingWakesPassengersDownTheRoadOfADrive(t *testing.T) {
	f := setup(t)
	occ := f.addOccurrence(t, corridorWKT)
	f.pinDrive(t, occ, driveDeparts)
	req := f.addRequest(t, func(o *requestOpts) {
		o.origin = match.Point{Lon: 25.90, Lat: 40.851}
		o.windowStart = driveDeparts.Add(5 * time.Minute)
		o.windowEnd = driveDeparts.Add(2 * time.Hour)
	})

	q := waitingQuery(occ)
	q.Now = driveDeparts.Add(-10 * time.Minute)
	got, err := match.Waiting(context.Background(), f.pool, q)
	if err != nil {
		t.Fatalf("Waiting: %v", err)
	}
	if len(got) != 1 || got[0] != req {
		t.Fatalf("a drive did not wake the passenger it can pick up: %v", got)
	}
}

func TestDriveDemandCountsOnlyWhoIsReady(t *testing.T) {
	f := setup(t)
	occ := f.addOccurrence(t, wktLine(25.80, 40.85, 26.00, 40.85),
		withWindow(driveDeparts, driveDeparts.Add(30*time.Minute)))
	now := driveDeparts.Add(-10 * time.Minute)

	// More people who cannot make it than any shortlist, created first so a
	// capped scan would see only them.
	for i := 0; i < 25; i++ {
		f.addRequest(t, func(o *requestOpts) {
			o.windowStart = driveDeparts.Add(20 * time.Minute) // after the car has passed
			o.windowEnd = driveDeparts.Add(2 * time.Hour)
			o.createdAt = now.Add(-time.Hour)
		})
	}
	n, err := match.DriveDemand(context.Background(), f.pool, occ, now)
	if err != nil {
		t.Fatalf("DriveDemand: %v", err)
	}
	if n != 0 {
		t.Fatalf("counted %d people who are only ready after the car has passed", n)
	}

	f.addRequest(t, func(o *requestOpts) {
		o.windowStart = now
		o.windowEnd = driveDeparts.Add(2 * time.Hour)
		o.createdAt = now
	})
	if n, err = match.DriveDemand(context.Background(), f.pool, occ, now); err != nil || n != 1 {
		t.Fatalf("DriveDemand = %d, %v; want the one passenger ready now", n, err)
	}
}

// The estimate ignores the stops before this one, so the late side allows for
// every minute of detour the driver agreed to. A passenger who gives up at
// 07:08 cannot rely on a car due at 07:02 with ten minutes of other people's
// detours allowed in front of them; with no detour allowed, they can.
func TestADriveAllowsForTheDetoursBeforeThePickup(t *testing.T) {
	f := setup(t)
	occ := f.addOccurrence(t, corridorWKT)
	f.pinDrive(t, occ, driveDeparts)
	q := driveQuery(driveDeparts.Add(-30*time.Minute), driveDeparts.Add(8*time.Minute))

	if _, err := f.pool.Exec(context.Background(),
		`UPDATE trip_occurrences SET detour_min = 10 WHERE id = $1`, occ); err != nil {
		t.Fatal(err)
	}
	if got := driveGross(t, f, q); len(got) != 0 {
		t.Fatalf("ten minutes of allowed detours were not counted: %+v", got)
	}
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE trip_occurrences SET detour_min = 1 WHERE id = $1`, occ); err != nil {
		t.Fatal(err)
	}
	if got := driveGross(t, f, q); len(got) != 1 {
		t.Fatalf("control: with a minute's detour the car is in time: %+v", got)
	}
}

// ---------------------------------------------------------- moving drive ---

// fix puts the driver's last position on the trip: lon along the corridor,
// how good it is, and when the phone took it.
func (f *fixture) fix(t *testing.T, occurrenceID uuid.UUID, lon, lat, accuracy float64, seen time.Time) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO trip_live_positions (occurrence_id, user_id, lat, lon, updated_at, accuracy_m, observed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $5)
		ON CONFLICT (occurrence_id, user_id) DO UPDATE
		SET lat = EXCLUDED.lat, lon = EXCLUDED.lon, updated_at = EXCLUDED.updated_at,
		    accuracy_m = EXCLUDED.accuracy_m, observed_at = EXCLUDED.observed_at`,
		occurrenceID, f.driverID, lat, lon, seen, accuracy); err != nil {
		t.Fatalf("placing the car: %v", err)
	}
}

// Six minutes after leaving, the car is three tenths of the way along. A
// passenger seven tenths along is eight minutes ahead of it and has time to
// reach the road; one two tenths along is behind it.
func movingQuery(originLon float64) match.GrossQuery {
	q := driveQuery(driveDeparts.Add(-30*time.Minute), driveDeparts.Add(2*time.Hour))
	q.Now = driveDeparts.Add(6 * time.Minute)
	q.Origin = match.Point{Lon: originLon, Lat: 40.851}
	q.RequestKm = 3
	return q
}

func TestAMovingDriveIsOfferedToSomebodyAheadOfTheCar(t *testing.T) {
	f := setup(t)
	occ := f.addOccurrence(t, corridorWKT)
	f.pinDrive(t, occ, driveDeparts)
	f.fix(t, occ, 25.86, 40.85, 20, driveDeparts.Add(5*time.Minute+50*time.Second))

	if got := driveGross(t, f, movingQuery(25.94)); len(got) != 1 {
		t.Fatalf("somebody eight minutes ahead of the car was not offered it: %+v", got)
	}
	if got := driveGross(t, f, movingQuery(25.84)); len(got) != 0 {
		t.Fatalf("somebody the car has already passed was offered it: %+v", got)
	}
}

// Without a fix it can trust, a car that has left is not offered to anybody:
// the planned time is a guess that gets worse every minute.
func TestAMovingDriveNeedsAFixItCanTrust(t *testing.T) {
	seen := driveDeparts.Add(5*time.Minute + 50*time.Second)
	cases := map[string]func(f *fixture, occ uuid.UUID){
		"no fix":          func(f *fixture, occ uuid.UUID) {},
		"three minutes old": func(f *fixture, occ uuid.UUID) { f.fix(t, occ, 25.86, 40.85, 20, seen.Add(-3*time.Minute)) },
		"half a km off":   func(f *fixture, occ uuid.UUID) { f.fix(t, occ, 25.86, 40.85, 500, seen) },
		"another road":    func(f *fixture, occ uuid.UUID) { f.fix(t, occ, 25.86, 40.87, 20, seen) },
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			occ := f.addOccurrence(t, corridorWKT)
			f.pinDrive(t, occ, driveDeparts)
			arrange(f, occ)
			if got := driveGross(t, f, movingQuery(25.94)); len(got) != 0 {
				t.Fatalf("offered on %s: %+v", name, got)
			}
		})
	}
}

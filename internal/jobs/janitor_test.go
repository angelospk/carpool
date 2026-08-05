package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/domain"
	"carpool/internal/geo"
	"carpool/internal/jobs"
	"carpool/internal/match"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

// The instant every retention test reasons from. A date rather than a duration
// offset, because the cutoffs are calendar intervals and "six months before"
// only means something from a real date.
var retentionNow = time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

// phoneNo returns distinct valid E.164 numbers, because otp_codes has a unique
// index over unconsumed codes per phone and a test that reuses one number would
// be testing the index rather than the janitor.
func phoneNo(i int) string { return fmt.Sprintf("+3069000001%02d", i) }

type janitorFixture struct {
	t     *testing.T
	pool  *pgxpool.Pool
	clock *clock.Fake
	user  uuid.UUID
}

func newJanitorFixture(t *testing.T) *janitorFixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var regionID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO regions (name, boundary, fuel_price_eur)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25.3 40.3, 26.7 40.3, 26.7 41.8, 25.3 41.8, 25.3 40.3))', 4326)), 1.8)
		RETURNING id`).Scan(&regionID); err != nil {
		t.Fatalf("seeding region: %v", err)
	}

	var userID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (phone, name, home_region_id) VALUES ('+306900000001', 'Driver', $1) RETURNING id`,
		regionID).Scan(&userID); err != nil {
		t.Fatalf("seeding user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO driver_profiles (user_id, car_model, car_plate) VALUES ($1, 'Yaris', 'ABC-1234')`,
		userID); err != nil {
		t.Fatalf("seeding driver profile: %v", err)
	}

	return &janitorFixture{t: t, pool: pool, clock: clock.NewFake(retentionNow), user: userID}
}

func (f *janitorFixture) run() {
	f.t.Helper()
	j := jobs.NewJanitor(f.pool, f.clock)
	if err := j.Run(context.Background()); err != nil {
		f.t.Fatalf("Janitor.Run: %v", err)
	}
}

func (f *janitorFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("exec %q: %v", sql, err)
	}
}

func (f *janitorFixture) count(table string) int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		f.t.Fatalf("counting %s: %v", table, err)
	}
	return n
}

// A verification record is kept for six months, which is what the privacy
// policy promises, and then it goes. The row on the near side of the cutoff has
// to survive, or the test would pass for a janitor that simply empties the
// table.
func TestVerificationRecordsGoAfterSixMonths(t *testing.T) {
	f := newJanitorFixture(t)
	old := retentionNow.AddDate(0, -6, -1)
	recent := retentionNow.AddDate(0, -6, 1)

	for i, at := range []time.Time{old, recent} {
		f.exec(`INSERT INTO otp_codes (phone, code_hash, expires_at, created_at)
		        VALUES ($1, 'h', $2, $2)`, phoneNo(i), at)
		f.exec(`INSERT INTO phone_change_challenges
		          (user_id, old_phone, new_phone, code_hmac, expires_at, created_at)
		        VALUES ($1, '+306900000001', '+306900000002', decode(repeat('61', 32), 'hex'), $2, $2)`,
			f.user, at)
	}

	f.run()

	if got := f.count("otp_codes"); got != 1 {
		t.Errorf("otp_codes: kept %d rows, want only the one inside six months", got)
	}
	if got := f.count("phone_change_challenges"); got != 1 {
		t.Errorf("phone_change_challenges: kept %d rows, want only the one inside six months", got)
	}
}

// Six months to the day is still inside the promise. Deleting on the boundary
// would be a day early every time, which is the kind of off-by-one nobody
// notices until it is the row somebody needed.
func TestARecordExactlyOnTheCutoffIsKept(t *testing.T) {
	f := newJanitorFixture(t)
	f.exec(`INSERT INTO otp_codes (phone, code_hash, expires_at, created_at)
	        VALUES ('+306900000009', 'h', $1, $1)`, retentionNow.AddDate(0, -6, 0))

	f.run()

	if got := f.count("otp_codes"); got != 1 {
		t.Errorf("a code exactly six months old was deleted; want kept")
	}
}

// Refresh tokens are credentials rather than records, so they go a month after
// they stop being usable. Both ways of stopping being usable count.
func TestSpentRefreshTokensGoAMonthLater(t *testing.T) {
	f := newJanitorFixture(t)
	long := retentionNow.AddDate(0, 0, -31)
	recent := retentionNow.AddDate(0, 0, -29)
	future := retentionNow.AddDate(0, 0, 30)

	insert := func(name string, expires time.Time, revoked *time.Time) {
		f.exec(`INSERT INTO refresh_tokens (user_id, token_hash, family_id, expires_at, revoked_at)
		        VALUES ($1, $2, gen_random_uuid(), $3, $4)`, f.user, name, expires, revoked)
	}
	insert("expired-long-ago", long, nil)
	insert("expired-recently", recent, nil)
	insert("live", future, nil)
	insert("revoked-long-ago", future, &long)
	insert("revoked-recently", future, &recent)

	f.run()

	var kept []string
	rows, err := f.pool.Query(context.Background(), `SELECT token_hash FROM refresh_tokens ORDER BY token_hash`)
	if err != nil {
		t.Fatalf("listing tokens: %v", err)
	}
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		kept = append(kept, h)
	}
	rows.Close()

	want := []string{"expired-recently", "live", "revoked-recently"}
	if len(kept) != len(want) {
		t.Fatalf("kept %v, want %v", kept, want)
	}
	for i := range want {
		if kept[i] != want[i] {
			t.Fatalf("kept %v, want %v", kept, want)
		}
	}
}

// seedThread creates a departed trip with messages at the given instants, and
// returns its occurrence id.
func (f *janitorFixture) seedThread(departed time.Time, messages ...time.Time) uuid.UUID {
	f.t.Helper()
	ctx := context.Background()

	templateID, err := store.CreateRouteTemplate(ctx, f.pool, store.CreateRouteTemplateParams{
		DriverID:         f.user,
		Origin:           geo.Point{Lon: 25.80, Lat: 40.85},
		Destination:      geo.Point{Lon: 26.00, Lat: 40.85},
		OriginLabel:      "Αλεξανδρούπολη",
		DestinationLabel: "Φέρες",
		RouteWKT:         corridorWKT,
		RouteKm:          17,
		RouteMin:         20,
		Seats:            3,
		PricingMode:      domain.PricingFormula,
		OSRMDataVersion:  "test",
		Detour:           match.Budget{AllowStops: true, Km: 10, Min: 10},
		Schedule: domain.Schedule{
			Kind:           domain.ScheduleRecurring,
			Weekdays:       []time.Weekday{time.Monday},
			DepartEarliest: domain.TimeOfDay{Hour: 7, Minute: 30},
			DepartLatest:   domain.TimeOfDay{Hour: 8, Minute: 0},
			EffectiveFrom:  domain.Date{Year: 2020, Month: time.January, Day: 1},
		},
	})
	if err != nil {
		f.t.Fatalf("seeding template: %v", err)
	}

	var occurrenceID uuid.UUID
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO trip_occurrences (template_id, service_date, depart_earliest, depart_latest,
		                              seats_total, seats_free, template_revision,
		                              route_geom, route_km, route_min, pricing_mode)
		VALUES ($1, $2::date, $2, $2, 3, 3, 1,
		        ST_GeomFromText('`+corridorWKT+`', 4326), 17, 20, 'formula')
		RETURNING id`, templateID, departed).Scan(&occurrenceID); err != nil {
		f.t.Fatalf("seeding occurrence: %v", err)
	}

	for _, at := range messages {
		f.exec(`INSERT INTO trip_messages (occurrence_id, sender_id, body, created_at)
		        VALUES ($1, $2, 'γεια', $3)`, occurrenceID, f.user, at)
	}
	return occurrenceID
}

// A conversation about a trip that ran over a year ago and has been silent
// since is deleted whole.
func TestASilentYearOldThreadIsDeleted(t *testing.T) {
	f := newJanitorFixture(t)
	f.seedThread(retentionNow.AddDate(0, -14, 0),
		retentionNow.AddDate(0, -14, 0), retentionNow.AddDate(0, -13, 0))

	f.run()

	if got := f.count("trip_messages"); got != 0 {
		t.Errorf("kept %d messages from a thread silent for over a year", got)
	}
}

// One message inside the year keeps the whole conversation, including the parts
// of it that are older than the cutoff. Deleting half a conversation leaves the
// remaining half misleading: an answer with no question above it.
func TestOneRecentMessageKeepsTheWholeThread(t *testing.T) {
	f := newJanitorFixture(t)
	f.seedThread(retentionNow.AddDate(0, -14, 0),
		retentionNow.AddDate(0, -14, 0),
		retentionNow.AddDate(0, -13, 0),
		retentionNow.AddDate(0, -1, 0))

	f.run()

	if got := f.count("trip_messages"); got != 3 {
		t.Errorf("kept %d of 3 messages; one message inside the year keeps the thread", got)
	}
}

// A trip that has not run yet is never eligible, however long ago the
// conversation about it started. Retention runs from the trip, not from the
// chatter before it.
func TestAnUpcomingTripKeepsItsMessages(t *testing.T) {
	f := newJanitorFixture(t)
	// Booked far in advance and discussed at the time, then silence.
	f.seedThread(retentionNow.AddDate(0, 1, 0), retentionNow.AddDate(0, -13, 0))

	f.run()

	if got := f.count("trip_messages"); got != 1 {
		t.Errorf("deleted the messages of a trip that has not departed")
	}
}

// The delete loops in batches, and the loop has to keep going past the first
// one. A batch size of one makes that provable without inserting thousands of
// rows to find out.
func TestDeletionContinuesPastTheFirstBatch(t *testing.T) {
	f := newJanitorFixture(t)
	old := retentionNow.AddDate(0, -7, 0)
	for i := range 5 {
		f.exec(`INSERT INTO otp_codes (phone, code_hash, expires_at, created_at)
		        VALUES ($1, 'h', $2, $2)`, phoneNo(i), old)
	}

	j := jobs.NewJanitor(f.pool, f.clock)
	j.BatchSize = 1
	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("Janitor.Run: %v", err)
	}

	if got := f.count("otp_codes"); got != 0 {
		t.Errorf("stopped after one batch: %d rows left", got)
	}
}

// Running twice changes nothing the second time. A janitor that is not
// idempotent is one that cannot be safely retried after a partial failure.
func TestASecondRunIsAQuietOne(t *testing.T) {
	f := newJanitorFixture(t)
	f.exec(`INSERT INTO otp_codes (phone, code_hash, expires_at, created_at)
	        VALUES ('+306900000009', 'h', $1, $1)`, retentionNow.AddDate(0, -7, 0))

	f.run()
	f.run()

	if got := f.count("otp_codes"); got != 0 {
		t.Errorf("second run left %d rows", got)
	}
}

// A run on a cancelled context deletes nothing and reports why.
//
// What this pins is the observable behaviour at shutdown, not the mechanism:
// the run gives up at the first thing it needs the database for, which is the
// advisory lock. Deleting a row while the process is going down would be a
// deletion nobody logged the result of.
func TestCancellationStopsTheSweep(t *testing.T) {
	f := newJanitorFixture(t)
	f.exec(`INSERT INTO otp_codes (phone, code_hash, expires_at, created_at)
	        VALUES ('+306900000009', 'h', $1, $1)`, retentionNow.AddDate(0, -7, 0))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	j := jobs.NewJanitor(f.pool, f.clock)
	if err := j.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run on a cancelled context: got %v, want context.Canceled", err)
	}
	if got := f.count("otp_codes"); got != 1 {
		t.Errorf("deleted %d rows after cancellation", 1-got)
	}
}

// Two janitors at once must not both sweep. The second finds the advisory lock
// taken and returns, rather than queueing behind the first and repeating it.
func TestOnlyOneJanitorSweepsAtATime(t *testing.T) {
	f := newJanitorFixture(t)

	held, err := f.pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquiring a connection: %v", err)
	}
	defer held.Release()
	var got bool
	if err := held.QueryRow(context.Background(),
		`SELECT pg_try_advisory_lock($1, $2)`, jobs.JanitorLockNamespace, jobs.JanitorLockID).Scan(&got); err != nil {
		t.Fatalf("taking the lock: %v", err)
	}
	if !got {
		t.Fatal("could not take the janitor lock in the test")
	}

	f.exec(`INSERT INTO otp_codes (phone, code_hash, expires_at, created_at)
	        VALUES ('+306900000009', 'h', $1, $1)`, retentionNow.AddDate(0, -7, 0))

	// Runs, finds the lock held, and returns without error: another replica is
	// already doing the work.
	f.run()

	if got := f.count("otp_codes"); got != 1 {
		t.Errorf("swept while another janitor held the lock")
	}
}

// A signup ticket is a credential with a ten-minute life. Once it has expired
// it grants nothing, so keeping it is keeping something that can only leak.
//
// A day's grace rather than none, so that a support question about a signup
// that failed this morning can still be answered — and so the sweep is not
// racing the ten-minute expiry of tickets people are holding right now.
func TestSignupTicketsGoTheDayAfterTheyDie(t *testing.T) {
	f := newJanitorFixture(t)

	for i, expires := range []time.Time{
		retentionNow.Add(-25 * time.Hour), // dead yesterday
		retentionNow.Add(-23 * time.Hour), // dead, but only just
		retentionNow.Add(5 * time.Minute), // somebody is typing their name
	} {
		f.exec(`INSERT INTO signup_tickets (token_hash, phone, expires_at)
		        VALUES ($1, $2, $3)`, fmt.Sprintf("hash-%d", i), phoneNo(i), expires)
	}

	f.run()

	if got := f.count("signup_tickets"); got != 2 {
		t.Errorf("signup_tickets: kept %d rows, want the two inside a day of expiry", got)
	}
}

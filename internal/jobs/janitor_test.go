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
	"carpool/internal/dispatch"
	"carpool/internal/domain"
	"carpool/internal/geo"
	"carpool/internal/jobs"
	"carpool/internal/match"
	"carpool/internal/outbox"
	"carpool/internal/push"
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
		`INSERT INTO users (date_of_birth, phone, name, home_region_id) VALUES (DATE '1990-01-01', '+306900000001', 'Driver', $1) RETURNING id`,
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
		VehicleID:        testdb.DefaultCar(f.t, f.pool, f.user),
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
		INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest, depart_latest,
		                              seats_total, seats_free, template_revision,
		                              route_geom, route_km, route_min, pricing_mode)
		VALUES ($1, (SELECT vehicle_id FROM route_templates WHERE id = $1), $2::date, $2, $2, 3, 3, 1,
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

// "Tokens ειδοποιήσεων — το πολύ 24 μήνες από την τελευταία χρήση". Nothing
// recorded the last use until now, so the second half of that promise could not
// be kept at all.
func TestAPushTokenGoesTwoYearsAfterItsLastUse(t *testing.T) {
	f := newJanitorFixture(t)
	old := retentionNow.AddDate(-2, 0, -1)
	recent := retentionNow.AddDate(-2, 0, 1)

	// Created long ago, used recently: the use is what counts.
	f.exec(`INSERT INTO push_tokens (token, user_id, platform, created_at, last_used_at)
	        VALUES ('used-recently', $1, 'ios', $2, $3)`, f.user, old, recent)
	// Created long ago, used long ago.
	f.exec(`INSERT INTO push_tokens (token, user_id, platform, created_at, last_used_at)
	        VALUES ('long-dead', $1, 'ios', $2, $2)`, f.user, old)
	// Never used, and old. The fallback to created_at is what makes this
	// reachable at all: a predicate on a null column is never true, and every
	// token that predates the column would otherwise live for ever.
	f.exec(`INSERT INTO push_tokens (token, user_id, platform, created_at)
	        VALUES ('never-used-old', $1, 'android', $2)`, f.user, old)
	// Never used, and new.
	f.exec(`INSERT INTO push_tokens (token, user_id, platform, created_at)
	        VALUES ('never-used-new', $1, 'android', $2)`, f.user, recent)

	f.run()

	var left []string
	rows, err := f.pool.Query(context.Background(), `SELECT token FROM push_tokens ORDER BY token`)
	if err != nil {
		t.Fatalf("listing tokens: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tok string
		if err := rows.Scan(&tok); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		left = append(left, tok)
	}

	want := []string{"never-used-new", "used-recently"}
	if fmt.Sprint(left) != fmt.Sprint(want) {
		t.Errorf("tokens left = %v, want %v", left, want)
	}
}

// "Αναφορές και αποκλεισμοί — 36 μήνες", measured from when the report was
// filed. A moderator looking at it later, or deciding it, does not restart the
// clock: the retention is about the event, not about the paperwork.
func TestAReportGoesAfterThirtySixMonths(t *testing.T) {
	f := newJanitorFixture(t)
	other := f.secondUser("+306900000077")

	f.exec(`INSERT INTO reports (reporter_id, reported_id, reason, created_at)
	        VALUES ($1, $2, 'harassment: old', $3)`, f.user, other, retentionNow.AddDate(-3, 0, -1))
	f.exec(`INSERT INTO reports (reporter_id, reported_id, reason, created_at)
	        VALUES ($1, $2, 'harassment: recent', $3)`, f.user, other, retentionNow.AddDate(-3, 0, 1))
	// Reviewed a moment ago, but filed three years and a day ago. Still goes:
	// working a queue must not grant its oldest items another three years.
	f.exec(`INSERT INTO reports (reporter_id, reported_id, reason, created_at,
	                             status, reviewed_by, reviewed_at, resolution)
	        VALUES ($1, $2, 'harassment: old but recently reviewed', $3,
	                'dismissed', $1, $4, 'nothing in it')`,
		f.user, other, retentionNow.AddDate(-3, 0, -1), retentionNow.Add(-time.Hour))

	f.run()

	if n := f.count("reports"); n != 1 {
		t.Errorf("%d reports left, want 1", n)
	}
	var reason string
	if err := f.pool.QueryRow(context.Background(), `SELECT reason FROM reports`).Scan(&reason); err != nil {
		t.Fatalf("reading the survivor: %v", err)
	}
	if reason != "harassment: recent" {
		t.Errorf("the wrong report survived: %q", reason)
	}
}

// An invite code ages from its last redemption, not from when it was made. A
// code somebody used last month is live however long ago it was printed.
func TestAnInviteCodeGoesTwoYearsAfterItsLastUse(t *testing.T) {
	f := newJanitorFixture(t)
	old := retentionNow.AddDate(-2, 0, -1)
	recent := retentionNow.AddDate(-2, 0, 1)

	f.exec(`INSERT INTO invite_codes (code, created_at, max_uses) VALUES ('OLDUNUSED', $1, 1)`, old)
	f.exec(`INSERT INTO invite_codes (code, created_at, max_uses) VALUES ('NEWUNUSED', $1, 1)`, recent)
	f.exec(`INSERT INTO invite_codes (code, created_at, max_uses) VALUES ('OLDUSEDNEW', $1, 5)`, old)

	other := f.secondUser("+306900000078")
	f.exec(`INSERT INTO invite_redemptions (code, user_id, redeemed_at) VALUES ('OLDUSEDNEW', $1, $2)`,
		other, recent)

	f.run()

	var left []string
	rows, err := f.pool.Query(context.Background(), `SELECT code FROM invite_codes ORDER BY code`)
	if err != nil {
		t.Fatalf("listing codes: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		left = append(left, c)
	}

	want := []string{"NEWUNUSED", "OLDUSEDNEW"}
	if fmt.Sprint(left) != fmt.Sprint(want) {
		t.Errorf("codes left = %v, want %v", left, want)
	}
}

// secondUser makes another account to be the other party in a report or a
// redemption.
func (f *janitorFixture) secondUser(phone string) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	err := f.pool.QueryRow(context.Background(),
		`INSERT INTO users (date_of_birth, phone, name, home_region_id)
		 SELECT DATE '1990-01-01', $1, 'Άλλος', home_region_id FROM users WHERE id = $2
		 RETURNING id`, phone, f.user).Scan(&id)
	if err != nil {
		f.t.Fatalf("seeding a second user: %v", err)
	}
	return id
}

// A rule that ages tokens by last use is only as good as the recording of it.
// Without this, every token looks unused, and the retention rule quietly
// becomes "delete every device after two years" — which would silently stop
// notifications for people who use the app every day.
func TestASuccessfulSendMarksTheTokenUsed(t *testing.T) {
	f := newJanitorFixture(t)
	long := retentionNow.AddDate(-2, 0, -1)
	f.exec(`INSERT INTO push_tokens (token, user_id, platform, created_at) VALUES ('live', $1, 'ios', $2)`,
		f.user, long)

	// One notification, delivered.
	f.deliverOneNotification("live")

	var lastUsed *time.Time
	if err := f.pool.QueryRow(context.Background(),
		`SELECT last_used_at FROM push_tokens WHERE token = 'live'`).Scan(&lastUsed); err != nil {
		t.Fatalf("reading last_used_at: %v", err)
	}
	if lastUsed == nil {
		t.Fatal("a delivered notification did not mark the token used")
	}

	// And so the janitor leaves it alone, though it was registered two years ago.
	f.run()
	if n := f.count("push_tokens"); n != 1 {
		t.Errorf("a token used moments ago was swept: %d left", n)
	}
}

// deliverOneNotification pushes a single message through the real dispatch
// router with a sender that accepts everything, which is the path that has to
// record the use.
func (f *janitorFixture) deliverOneNotification(token string) {
	f.t.Helper()
	router := dispatch.NewRouter(f.pool, f.clock, acceptingSender{},
		func(context.Context, uuid.UUID) error { return nil })

	// An ask arriving for this driver, which is the shape that names them as
	// the recipient.
	payload := fmt.Sprintf(`{"ask_id":%q,"driver_id":%q,"detour_min":5}`, uuid.New(), f.user)
	err := router.Handle(context.Background(), outbox.Message{
		Kind: "push", Payload: []byte(payload), IdempotencyKey: "test-1",
	})
	if err != nil {
		f.t.Fatalf("delivering a notification: %v", err)
	}
	_ = token
}

// acceptingSender is a push provider that delivers everything.
type acceptingSender struct{}

func (acceptingSender) Send(context.Context, push.Notification) error { return nil }

// "Διαδρομές, αιτήματα, γεωμετρία — ιστορικό 24 μήνες", anchored on the
// departure rather than on when the route was created. A trip planned a long
// time ago for next week is not history; a trip that ran two years ago is,
// however recently its route was edited.
func TestATripGoesTwoYearsAfterItRan(t *testing.T) {
	f := newJanitorFixture(t)
	old := f.departure(retentionNow.AddDate(-2, 0, -1))
	recent := f.departure(retentionNow.AddDate(-2, 0, 1))
	future := f.departure(retentionNow.AddDate(0, 1, 0))

	f.run()

	if f.occurrenceExists(old) {
		t.Error("a departure from more than two years ago survived")
	}
	if !f.occurrenceExists(recent) {
		t.Error("a departure from just under two years ago was swept")
	}
	if !f.occurrenceExists(future) {
		t.Error("a departure that has not happened yet was swept")
	}
}

// The reason migration 00028 exists. Ratings live as long as the account; trips
// live 24 months. Without breaking the cascade, sweeping the trip would take
// the rating with it and a driver's reputation would erode by age.
func TestSweepingATripLeavesTheRatingsBehind(t *testing.T) {
	f := newJanitorFixture(t)
	occ := f.departure(retentionNow.AddDate(-2, 0, -1))
	other := f.secondUser("+306900000081")
	booking := f.booking(occ, other)

	f.exec(`INSERT INTO ratings (booking_id, rater_id, ratee_id, stars, tag)
	        VALUES ($1, $2, $3, 5, 'punctual')`, booking, other, f.user)

	f.run()

	if f.occurrenceExists(occ) {
		t.Fatal("the old departure survived, so this proves nothing")
	}
	var stars int
	var bookingID *uuid.UUID
	err := f.pool.QueryRow(context.Background(),
		`SELECT stars, booking_id FROM ratings WHERE ratee_id = $1`, f.user).Scan(&stars, &bookingID)
	if err != nil {
		t.Fatalf("the rating went with the trip: %v", err)
	}
	if stars != 5 {
		t.Errorf("stars = %d", stars)
	}
	if bookingID != nil {
		t.Error("the rating still points at a booking that was deleted")
	}
}

// Messages have their own twelve-month clock, anchored on the last message as
// well as on the departure. A trip is held back rather than cascading them
// away — bounded, because the message rule will clear them and the trip
// becomes eligible at a later run.
func TestATripWaitsForItsMessages(t *testing.T) {
	f := newJanitorFixture(t)
	occ := f.departure(retentionNow.AddDate(-2, 0, -1))
	f.exec(`INSERT INTO trip_messages (occurrence_id, sender_id, body, created_at)
	        VALUES ($1, $2, 'άφησα το μπουφάν μου στο αυτοκίνητο', $3)`,
		occ, f.user, retentionNow.AddDate(0, -1, 0))

	f.run()

	if !f.occurrenceExists(occ) {
		t.Error("a trip with a month-old message on it was swept, taking the message")
	}
}

// A report is kept for 36 months and a trip for 24. Holding the trip back keeps
// the evidence attached for the whole life of the report, rather than leaving an
// accusation with a null where the booking was.
func TestATripWaitsForAReportAboutIt(t *testing.T) {
	f := newJanitorFixture(t)
	occ := f.departure(retentionNow.AddDate(-2, 0, -1))
	other := f.secondUser("+306900000082")
	booking := f.booking(occ, other)

	f.exec(`INSERT INTO reports (reporter_id, reported_id, booking_id, reason, created_at)
	        VALUES ($1, $2, $3, 'no_show: δεν ήρθε ποτέ', $4)`,
		other, f.user, booking, retentionNow.AddDate(-1, 0, 0))

	f.run()

	if !f.occurrenceExists(occ) {
		t.Error("the trip a live report is about was swept, leaving the report with nothing")
	}
}

// An old request that nobody ever matched still goes, and one whose window has
// not opened yet does not.
func TestAnOldRideRequestGoes(t *testing.T) {
	f := newJanitorFixture(t)
	old := f.request(retentionNow.AddDate(-2, 0, -1))
	recent := f.request(retentionNow.AddDate(-2, 0, 1))

	f.run()

	if f.requestExists(old) {
		t.Error("a request from more than two years ago survived")
	}
	if !f.requestExists(recent) {
		t.Error("a request from just under two years ago was swept")
	}
}

// departure seeds one occurrence leaving at the given instant.
func (f *janitorFixture) departure(at time.Time) uuid.UUID {
	f.t.Helper()
	return f.seedThread(at)
}

func (f *janitorFixture) occurrenceExists(id uuid.UUID) bool {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM trip_occurrences WHERE id = $1`, id).Scan(&n); err != nil {
		f.t.Fatalf("looking for the occurrence: %v", err)
	}
	return n > 0
}

// booking puts a passenger on a departure, which is what a rating or a report
// hangs off.
func (f *janitorFixture) booking(occurrence, passenger uuid.UUID) uuid.UUID {
	f.t.Helper()
	var requestID, bookingID uuid.UUID
	err := f.pool.QueryRow(context.Background(), `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end,
		                           seats, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		        $2::timestamptz, $2::timestamptz + interval '2 hours', 1, 'open', 'matched')
		RETURNING id`, passenger, retentionNow.AddDate(-2, 0, -1)).Scan(&requestID)
	if err != nil {
		f.t.Fatalf("seeding request: %v", err)
	}
	// A booking has to say where it came from: an ask, or a standing
	// arrangement, never neither.
	var askID uuid.UUID
	err = f.pool.QueryRow(context.Background(), `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km,
		                  pickup_point, dropoff_point, suggested_price_eur,
		                  itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2,
		        ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		        2.50, 0, 'accepted', $3)
		RETURNING id`, requestID, occurrence, retentionNow.AddDate(-2, 0, -1)).Scan(&askID)
	if err != nil {
		f.t.Fatalf("seeding ask: %v", err)
	}

	err = f.pool.QueryRow(context.Background(), `
		INSERT INTO bookings (occurrence_id, request_id, ask_id, passenger_id, seats, price_eur,
		                      pickup_point, dropoff_point)
		VALUES ($1, $2, $3, $4, 1, 2.50,
		        ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326))
		RETURNING id`, occurrence, requestID, askID, passenger).Scan(&bookingID)
	if err != nil {
		f.t.Fatalf("seeding booking: %v", err)
	}
	return bookingID
}

// request seeds a standalone ride request whose travel window is at the given
// instant.
func (f *janitorFixture) request(at time.Time) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	err := f.pool.QueryRow(context.Background(), `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end,
		                           seats, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		        $2::timestamptz, $2::timestamptz + interval '2 hours', 1, 'open', 'expired')
		RETURNING id`, f.user, at).Scan(&id)
	if err != nil {
		f.t.Fatalf("seeding request: %v", err)
	}
	return id
}

func (f *janitorFixture) requestExists(id uuid.UUID) bool {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM ride_requests WHERE id = $1`, id).Scan(&n); err != nil {
		f.t.Fatalf("looking for the request: %v", err)
	}
	return n > 0
}

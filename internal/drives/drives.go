// Package drives lets a driver say when they are really leaving.
//
// A schedule says "between 07:00 and 07:30", and that is all the matcher has
// had to go on: it could not tell a passenger when the car would reach them,
// and it stopped offering a trip the moment its window opened, so a driver
// running late could not be joined. A drive pins the departure to one minute.
// The window becomes that minute, every rule that already reads the window is
// right for it without being touched, and the matcher can hold a pickup to the
// one rule that matters on the day: the passenger is at the road before the
// car, and the driver never waits. See specs/DRIVING-NOW.md.
//
// The nudge is the other half. A driver who asked for it is told, shortly
// before a departure, that somebody on their road is waiting — a count, never
// a name — so they can say when they are leaving and those people are asked.
package drives

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/match"
	"carpool/internal/notices"
	"carpool/internal/outbox"
)

// ErrNotYours means the caller does not drive this trip, the trip does not
// exist, or the caller cannot drive at all just now. One error for all three,
// so trip ids cannot be probed.
var ErrNotYours = errors.New("drives: not your trip")

// ErrNotScheduled means the trip has been called off.
var ErrNotScheduled = errors.New("drives: that trip is not running")

// ErrDeparted means the minute the driver gave has passed: the car has gone.
var ErrDeparted = errors.New("drives: that drive has already left")

// ErrBadTime means the departure is not within the next quarter of an hour.
var ErrBadTime = errors.New("drives: leave within the next 15 minutes")

// ErrOutsideWindow means the minute is too far from the published window: before
// it or more than a quarter of an hour after it with passengers booked, an hour
// either side without.
var ErrOutsideWindow = errors.New("drives: too far from the published time")

// ErrBadDetour means a detour outside 0–30 minutes.
var ErrBadDetour = errors.New("drives: detour must be 0 to 30 minutes")

const (
	// Lead is how far ahead a driver can pin their departure. Further than
	// this is a plan, which is what the schedule is for.
	Lead = 15 * time.Minute
	// NudgeLead is how long before the window opens a driver is nudged.
	NudgeLead = 15 * time.Minute

	maxDetourMin = 30
	// slack forgives the phone's clock and the trip to the server: a "now"
	// tapped a few seconds ago arrives a few seconds in the past.
	slack = time.Minute
	// lateWithPassengers is how far past the published window a driver with
	// people booked may still say they are leaving. Those people agreed to the
	// window; a little late is life, and an hour is a different trip.
	lateWithPassengers = 15 * time.Minute
	// shiftWhenEmpty is how far either side of the window an empty car may
	// move. Further and it is not this departure any more.
	shiftWhenEmpty = time.Hour
)

// Service runs drives.
type Service struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewService builds a Service.
func NewService(pool *pgxpool.Pool, c clock.Clock) *Service {
	return &Service{pool: pool, clock: c}
}

// Start is what the driver says.
//
// The departure is a time rather than "in five minutes" so that a request the
// phone sends twice is the same request: a relative time computed on arrival
// would be a different minute each time, and the passengers would be told twice.
type Start struct {
	DepartsAt time.Time
	// DetourMin, when set, replaces how far out of their way the driver will go
	// for this departure. Zero means no stops off the road at all.
	DetourMin *int
}

// Start pins a departure and returns the minute it was pinned to.
//
// Locks are taken in the order every other path uses — the driver's user row,
// the occurrence, then the requests behind any pending offers, then the offers
// — and the clock is read after them, so a call that waited behind another one
// judges the time when it actually runs.
func (s *Service) Start(ctx context.Context, driverID, occurrenceID uuid.UUID, in Start) (time.Time, error) {
	if in.DetourMin != nil && (*in.DetourMin < 0 || *in.DetourMin > maxDetourMin) {
		return time.Time{}, ErrBadDetour
	}
	departs := in.DepartsAt.UTC().Truncate(time.Minute)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("drives: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM users WHERE id = $1 FOR SHARE`, driverID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "active") {
		return time.Time{}, ErrNotYours
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("drives: locking the driver: %w", err)
	}

	var (
		owner                  uuid.UUID
		occStatus              string
		earliest, latest       time.Time
		scheduledE, scheduledL time.Time
		driveSet               bool
		detourMin              int
		booked                 bool
	)
	err = tx.QueryRow(ctx, `
		SELECT t.driver_id, o.status, o.depart_earliest, o.depart_latest,
		       COALESCE(o.scheduled_earliest, o.depart_earliest),
		       COALESCE(o.scheduled_latest, o.depart_latest),
		       o.drive_set_at IS NOT NULL, o.detour_min
		  FROM trip_occurrences o
		  JOIN route_templates t ON t.id = o.template_id
		 WHERE o.id = $1
		 FOR UPDATE OF o`, occurrenceID).Scan(
		&owner, &occStatus, &earliest, &latest, &scheduledE, &scheduledL, &driveSet, &detourMin)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != driverID) {
		return time.Time{}, ErrNotYours
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("drives: locking the trip: %w", err)
	}
	if occStatus != "scheduled" {
		return time.Time{}, ErrNotScheduled
	}
	// Its own statement, after the lock. A subquery in the locking statement
	// keeps the snapshot it started with, so a booking committed while this
	// waited on the row would be missed, and the driver could leave before the
	// window that passenger had just agreed to.
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM bookings
		                WHERE occurrence_id = $1 AND status IN ('confirmed','completed'))`,
		occurrenceID).Scan(&booked); err != nil {
		return time.Time{}, fmt.Errorf("drives: checking for passengers: %w", err)
	}

	detourChanged := in.DetourMin != nil && *in.DetourMin != detourMin
	// The same minute asked for again is the same drive. Checked before the
	// clock, so a retry that arrives after the minute has passed still gets
	// the answer the first one got.
	if driveSet && earliest.Equal(departs) && latest.Equal(departs) && !detourChanged {
		return departs, tx.Commit(ctx)
	}

	now := s.clock.Now()
	if driveSet && !now.Before(earliest) {
		return time.Time{}, ErrDeparted
	}
	if departs.Before(now.Add(-slack)) || departs.After(now.Add(Lead)) {
		return time.Time{}, ErrBadTime
	}
	lo, hi := scheduledE.Add(-shiftWhenEmpty), scheduledL.Add(shiftWhenEmpty)
	if booked {
		lo, hi = scheduledE, scheduledL.Add(lateWithPassengers)
	}
	if departs.Before(lo) || departs.After(hi) {
		return time.Time{}, ErrOutsideWindow
	}

	if _, err := tx.Exec(ctx, `
		UPDATE trip_occurrences
		   SET scheduled_earliest = COALESCE(scheduled_earliest, depart_earliest),
		       scheduled_latest   = COALESCE(scheduled_latest, depart_latest),
		       depart_earliest    = $2,
		       depart_latest      = $2,
		       drive_set_at       = $3,
		       offers_until       = $2::timestamptz + route_min * interval '1 minute',
		       -- A driver choosing how far off their road to go is a driver
		       -- who stops for passengers, even at zero: a pickup on their own
		       -- road is still a stop.
		       -- Zero is "my road only", and pulling over on it still costs a
		       -- minute; a budget of nothing would refuse that pickup too.
		       detour_min         = CASE WHEN $4::int IS NULL THEN detour_min ELSE GREATEST($4::int, 1) END,
		       allow_stops        = allow_stops OR $4::int IS NOT NULL,
		       updated_at         = now()
		 WHERE id = $1`, occurrenceID, departs, now, in.DetourMin); err != nil {
		return time.Time{}, fmt.Errorf("drives: pinning the departure: %w", err)
	}

	if err := s.settleOffers(ctx, tx, occurrenceID, departs, now, detourChanged); err != nil {
		return time.Time{}, err
	}
	if err := outbox.Wake(ctx, tx, occurrenceID, "drive", now); err != nil {
		return time.Time{}, err
	}
	if !earliest.Equal(departs) || !latest.Equal(departs) {
		if err := s.tellPassengers(ctx, tx, occurrenceID, departs, now); err != nil {
			return time.Time{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, fmt.Errorf("drives: commit: %w", err)
	}
	return departs, nil
}

// settleOffers brings the offers already on the driver's phone into line with
// the new minute.
//
// One the passenger can no longer make, or one quoted under a detour the
// driver has just changed, is let go now: its expiry becomes this instant and
// the sweeper moves the passenger on to somebody else, by the same path as any
// other lapsed offer. The rest end at the last moment the passenger could
// still be told and reach the road first, like every offer made from here on. A later change of mind does not lengthen them again; an offer
// that has been cut short stays cut short.
func (s *Service) settleOffers(
	ctx context.Context, tx pgx.Tx, occurrenceID uuid.UUID, departs, now time.Time, detourChanged bool,
) error {
	// Requests before asks, in id order, as the sweeper and the accept path
	// take them.
	if _, err := tx.Exec(ctx, `
		SELECT rr.id FROM ride_requests rr
		 WHERE rr.id IN (SELECT a.request_id FROM asks a
		                  WHERE a.occurrence_id = $1 AND a.status = 'pending')
		 ORDER BY rr.id
		 FOR UPDATE`, occurrenceID); err != nil {
		return fmt.Errorf("drives: locking the requests behind open offers: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		SELECT id FROM asks WHERE occurrence_id = $1 AND status = 'pending'
		 ORDER BY id FOR UPDATE`, occurrenceID); err != nil {
		return fmt.Errorf("drives: locking open offers: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE asks a
		   SET expires_at = CASE
		         WHEN $4 OR NOT `+match.DriveReady("$2::timestamptz", "o.route_min",
		"ST_LineLocatePoint(o.route_geom, ST_ClosestPoint(o.route_geom, rr.origin))",
		"ST_Distance(o.route_geom::geography, rr.origin::geography)",
		"GREATEST(rr.window_start, $3::timestamptz)", "rr.window_end", "o.detour_min")+`
		         THEN LEAST(a.expires_at, $3::timestamptz)
		         ELSE LEAST(a.expires_at, `+match.DriveDeadline("$2::timestamptz", "o.route_min",
		"ST_LineLocatePoint(o.route_geom, ST_ClosestPoint(o.route_geom, rr.origin))",
		"ST_Distance(o.route_geom::geography, rr.origin::geography)")+`)
		       END
		  FROM ride_requests rr, trip_occurrences o
		 WHERE a.occurrence_id = $1 AND a.status = 'pending'
		   AND rr.id = a.request_id AND o.id = a.occurrence_id`,
		occurrenceID, departs, now, detourChanged); err != nil {
		return fmt.Errorf("drives: settling open offers: %w", err)
	}
	return nil
}

// tellPassengers tells everybody with a seat that the time has moved.
func (s *Service) tellPassengers(ctx context.Context, tx pgx.Tx, occurrenceID uuid.UUID, departs, now time.Time) error {
	rows, err := tx.Query(ctx, `
		SELECT id, passenger_id FROM bookings
		 WHERE occurrence_id = $1 AND status = 'confirmed' ORDER BY id`, occurrenceID)
	if err != nil {
		return fmt.Errorf("drives: listing passengers: %w", err)
	}
	type seat struct{ booking, passenger uuid.UUID }
	var seats []seat
	for rows.Next() {
		var st seat
		if err := rows.Scan(&st.booking, &st.passenger); err != nil {
			rows.Close()
			return fmt.Errorf("drives: scanning a passenger: %w", err)
		}
		seats = append(seats, st)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("drives: reading passengers: %w", err)
	}
	for _, st := range seats {
		msg, err := notices.Retimed(st.passenger, st.booking, departs)
		if err != nil {
			return err
		}
		if err := outbox.Enqueue(ctx, tx, msg, now); err != nil {
			return err
		}
	}
	return nil
}

// Nudge tells drivers who asked for it that somebody on their road is
// waiting, once per departure, and returns how many it told.
//
// A departure qualifies from NudgeLead before its window opens until it opens,
// while it has a seat nobody has been offered, the driver has not already said
// when they are leaving, and at least one person could be picked up if they
// left on time. Nothing is marked for a departure with nobody waiting, so it
// is looked at again on the next run: somebody may ask in the meantime.
func (s *Service) Nudge(ctx context.Context) (int, error) {
	now := s.clock.Now()
	rows, err := s.pool.Query(ctx, `
		SELECT o.id
		  FROM trip_occurrences o
		  JOIN route_templates t ON t.id = o.template_id
		 WHERE t.nudge_when_leaving
		   AND o.status = 'scheduled'
		   AND o.drive_set_at IS NULL
		   AND o.nudged_at IS NULL
		   AND o.depart_earliest > $1
		   AND o.depart_earliest <= $2
		 ORDER BY o.id`, now, now.Add(NudgeLead))
	if err != nil {
		return 0, fmt.Errorf("drives: listing departures to nudge for: %w", err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("drives: scanning a departure: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("drives: reading departures: %w", err)
	}

	sent := 0
	for _, id := range ids {
		ok, err := s.nudgeOne(ctx, id, now)
		if err != nil {
			return sent, err
		}
		if ok {
			sent++
		}
	}
	return sent, nil
}

// nudgeOne decides one departure under its lock, so two workers cannot both
// send, and the notice and the mark commit together or not at all.
//
// The driver's row is shared rather than skipped, the way every path that
// acts for a driver holds it, so a suspension landing now is decided first.
// Muting and the route's own switch are read rather than locked: a nudge that
// crosses the driver turning it off is one notification, not a broken promise.
func (s *Service) nudgeOne(ctx context.Context, occurrenceID uuid.UUID, now time.Time) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("drives: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var driverID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT u.id FROM users u
		  JOIN route_templates t ON t.driver_id = u.id
		  JOIN trip_occurrences o ON o.template_id = t.id
		 WHERE o.id = $1 AND u.status = 'active'
		 FOR SHARE OF u`, occurrenceID).Scan(&driverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("drives: locking the driver: %w", err)
	}

	var eligible bool
	err = tx.QueryRow(ctx, `
		SELECT t.nudge_when_leaving AND NOT d.notifications_muted
		       AND o.status = 'scheduled' AND o.drive_set_at IS NULL AND o.nudged_at IS NULL
		       AND o.depart_earliest > $2
		       AND o.seats_free - `+match.SeatsSpokenFor("o.id", "$2")+` > 0
		  FROM trip_occurrences o
		  JOIN route_templates t ON t.id = o.template_id
		  JOIN driver_profiles d ON d.user_id = t.driver_id
		 WHERE o.id = $1
		 FOR UPDATE OF o`, occurrenceID, now).Scan(&eligible)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !eligible) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("drives: locking the departure: %w", err)
	}

	// Counted through this transaction: asking the pool for a second
	// connection while holding the lock lets two workers each hold one and
	// wait for the other.
	count, err := match.DriveDemand(ctx, tx, occurrenceID, now)
	if err != nil {
		return false, err
	}
	if count == 0 {
		return false, nil
	}

	msg, err := notices.Nudge(driverID, occurrenceID, count)
	if err != nil {
		return false, err
	}
	if err := outbox.Enqueue(ctx, tx, msg, now); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE trip_occurrences SET nudged_at = $2 WHERE id = $1`, occurrenceID, now); err != nil {
		return false, fmt.Errorf("drives: marking the nudge: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("drives: commit: %w", err)
	}
	return true, nil
}

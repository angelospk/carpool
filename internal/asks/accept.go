// Package asks implements the sequential driver-ask lifecycle: creating an ask
// for the next candidate, accepting or declining it, and expiring it.
package asks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/match"
	"carpool/internal/outbox"
	"carpool/internal/tolls"
)

// DefaultTTL is how long a driver has to answer before the ask expires and the
// next candidate is approached.
const DefaultTTL = 30 * time.Minute

// MovingTTL is how long a driver on the road has to answer: a tap on the
// notification, or nothing, and the passenger moves on.
const MovingTTL = 2 * time.Minute

// Errors the accept path can return. They are distinct because the app shows a
// different message for each.
var (
	// ErrAskNotPending covers an ask that was already accepted, declined,
	// expired or cancelled.
	ErrAskNotPending = errors.New("asks: the ask is no longer pending")
	// ErrAskExpired means the TTL passed before the driver answered.
	ErrAskExpired = errors.New("asks: the ask has expired")
	// ErrAskNotFound means there is no such ask. The API answers it exactly as
	// it answers ErrNotYours: an ask that belongs to somebody else must not be
	// told apart from one that does not exist.
	ErrAskNotFound = errors.New("asks: no such ask")
	// ErrNoSeats means the occurrence filled up first.
	ErrNoSeats = errors.New("asks: the trip has no free seats")
	// ErrNoLuggageRoom means the seats are there but the boot is not. A separate
	// error from ErrNoSeats because it is a separate conversation: the driver
	// may well take this person with one bag instead of three.
	ErrNoLuggageRoom = errors.New("asks: the trip has no room left for that much luggage")
	// ErrStaleItinerary means the driver's plan changed while the detour was
	// being verified, so the quoted detour no longer applies.
	ErrStaleItinerary = errors.New("asks: the itinerary changed during verification")
	// ErrNotYours means the caller is not the driver being asked.
	ErrNotYours = errors.New("asks: the ask belongs to another driver")
	// ErrTripDeparted means the departure the ask was for is due to leave, so
	// there is no longer a seat to take. Distinct from ErrAskExpired because
	// what ran out is not the driver's half hour but the trip itself, and the
	// passenger has to be told to look for another ride rather than to wait.
	ErrTripDeparted = errors.New("asks: the trip has already left")
	// ErrAlreadyAboard means the passenger already has a seat on this very
	// departure through another request. One person, one seat per car: the
	// database refuses a second booking, and before this was checked here that
	// refusal reached the driver as "something went wrong".
	ErrAlreadyAboard = errors.New("asks: the passenger already has a seat on this trip")
	// ErrBusy means the departure's plan kept moving under every attempt to
	// accept. The ask is still open and pressing again is the answer.
	ErrBusy = errors.New("asks: the trip kept changing; try again")
)

// Verifier re-checks that a detour still fits the driver's budget, given the
// itinerary version it was computed against.
//
// It is called OUTSIDE the accept transaction: it needs an OSRM round trip, and
// holding row locks across a slow, failure-prone sidecar call would be a
// liveness hazard. The version check inside the transaction is what closes the
// resulting race — see RISKS.md open decision 4.
type Verifier interface {
	Verify(ctx context.Context, askID uuid.UUID, itineraryVersion int) error
}

// Service runs the ask lifecycle.
type Service struct {
	pool     *pgxpool.Pool
	clock    clock.Clock
	verifier Verifier
	router   match.Router
	// afterRequote, when set, runs inside Accept's transaction once the other
	// offers are re-quoted. Tests only: it holds two accepts at the point where
	// they would deadlock.
	afterRequote func()
	// tolls is the shipped toll network. Nil when the dataset failed to load,
	// which costs the toll component of the suggested contribution and nothing
	// else — the app must still match rides.
	tolls *tolls.Dataset
}

// NewService returns a service. verifier may be nil, in which case the stored
// detour is trusted as-is. router is required by AskNext but not by Accept.
func NewService(pool *pgxpool.Pool, c clock.Clock, v Verifier) *Service {
	s := &Service{pool: pool, clock: c, verifier: v}
	if set, err := tolls.Load(); err == nil {
		s.tolls = set
	}
	return s
}

// WithRouter returns a copy of the service that uses r for the fine matching
// stage.
func (s *Service) WithRouter(r match.Router) *Service {
	out := *s
	out.router = r
	return &out
}

// Accept books the seat an ask offers.
//
// Locks are taken in the canonical order occurrence → ride_request → ask.
// Every path that mutates these rows uses the same order, which is what keeps
// concurrent accepts, declines and sweeps from deadlocking.
//
// Two mechanisms guard against overbooking, deliberately. The row lock below
// makes the seat check and the decrement atomic, so a loser gets a clean
// ErrNoSeats. The seats_free >= 0 CHECK constraint in the schema is the actual
// backstop: mutation-testing this function with the lock removed still produced
// exactly one booking, because the constraint rejects the second decrement.
// The lock buys the good error message; the constraint buys the invariant.
//
// Every other pending offer on the departure is re-quoted against the plan
// this accept makes, in the same transaction: moved onto the new itinerary
// version with its new detour and price if it still fits, withdrawn and its
// passenger looked for elsewhere if not. The routing is done before any lock
// and checked against the version found under it; if the plan moved in
// between, the whole accept starts again.
func (s *Service) Accept(ctx context.Context, askID, driverID uuid.UUID) (uuid.UUID, error) {
	for attempt := 0; attempt < acceptAttempts; attempt++ {
		bookingID, err := s.accept(ctx, askID, driverID)
		if !errors.Is(err, errPlanMoved) {
			return bookingID, err
		}
	}
	return uuid.Nil, ErrBusy
}

func (s *Service) accept(ctx context.Context, askID, driverID uuid.UUID) (uuid.UUID, error) {
	// Read the version the ask was quoted against and the one the plan is at,
	// then verify and route outside any transaction.
	var (
		quotedVersion int
		planVersion   int
		preOccurrence uuid.UUID
		preState      string
		preExpiresAt  time.Time
		preDriver     uuid.UUID
		prePassenger  uuid.UUID
	)
	err := s.pool.QueryRow(ctx, `
		SELECT a.itinerary_version, o.itinerary_version, o.id, a.status, a.expires_at, t.driver_id, rr.passenger_id
		FROM asks a
		JOIN trip_occurrences o ON o.id = a.occurrence_id
		JOIN route_templates  t ON t.id = o.template_id
		JOIN ride_requests   rr ON rr.id = a.request_id
		WHERE a.id = $1`, askID).Scan(&quotedVersion, &planVersion, &preOccurrence,
		&preState, &preExpiresAt, &preDriver, &prePassenger)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrAskNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: loading ask: %w", err)
	}

	// Reject the obvious cases before spending an OSRM round trip. Anyone
	// holding an ask id could otherwise drive routing load with calls that were
	// always going to fail, and a declined ask would surface a routing error
	// instead of the documented ErrAskNotPending. These are all re-checked under
	// lock below; this is a cheap filter, not the authority.
	if preDriver != driverID {
		return uuid.Nil, ErrNotYours
	}
	// Once the sweeper has run the row says 'expired' and nothing else; the
	// driver answering a late push is told so, not that somebody answered it.
	if preState == "expired" {
		return uuid.Nil, ErrAskExpired
	}
	if preState != "pending" {
		return uuid.Nil, ErrAskNotPending
	}
	if !s.clock.Now().Before(preExpiresAt) {
		return uuid.Nil, ErrAskExpired
	}

	if s.verifier != nil {
		if err := s.verifier.Verify(ctx, askID, quotedVersion); err != nil {
			return uuid.Nil, err
		}
	}

	plan, err := s.planRequotes(ctx, preOccurrence, askID, planVersion)
	if err != nil {
		return uuid.Nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Before any row lock, as in createAsk: one passenger's seats and offers
	// are given out one at a time. With the passengers of the offers about to
	// be re-quoted, in one sorted order.
	for _, p := range plan.lockOrder(prePassenger) {
		if _, err := tx.Exec(ctx, match.PassengerLockSQL, p); err != nil {
			return uuid.Nil, fmt.Errorf("asks: locking the passenger's offers: %w", err)
		}
	}

	// Read the ask's foreign keys without locking so the locks below can be
	// taken in the canonical order regardless of which id the caller had.
	var occurrenceID, requestID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT occurrence_id, request_id FROM asks WHERE id = $1`, askID).Scan(&occurrenceID, &requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrAskNotPending
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: loading ask keys: %w", err)
	}

	// 1. both people. Before their rows, so that a suspension landing while this
	// runs is decided rather than raced: see lockParticipants. An offer already
	// sitting on a driver's phone is exactly how a suspended passenger ends up
	// with a seat held in their name.
	if err := lockParticipants(ctx, tx, driverID, prePassenger); err != nil {
		if errors.Is(err, errNotActive) || errors.Is(err, errBlocked) {
			return uuid.Nil, ErrAskNotPending
		}
		return uuid.Nil, err
	}
	// Women drivers only, read again now that both rows are held. The matcher
	// applies it when the offer is made; a passenger can turn it on, or a
	// driver change their gender, while the offer sits on a phone. Answered
	// like a block: the offer is simply no longer there.
	var unwanted bool
	if err := tx.QueryRow(ctx, `
		SELECT p.women_only AND d.gender IS DISTINCT FROM 'female'
		  FROM users p, users d
		 WHERE p.id = $1 AND d.id = $2`, prePassenger, driverID).Scan(&unwanted); err != nil {
		return uuid.Nil, fmt.Errorf("asks: reading the passenger's preference: %w", err)
	}
	if unwanted {
		return uuid.Nil, ErrAskNotPending
	}

	// A seat elsewhere at the same hour, or another live offer for it, since
	// this offer was made: an offer that lapsed at the last second lets the
	// passenger's other request be put to somebody else. The ask is void.
	var busy bool
	if err := tx.QueryRow(ctx, `SELECT `+match.PassengerHolds("o", "$2::uuid", "$3", "$4::uuid")+`
		  FROM trip_occurrences o WHERE o.id = $1`,
		occurrenceID, prePassenger, s.clock.Now(), askID).Scan(&busy); err != nil {
		return uuid.Nil, fmt.Errorf("asks: reading the passenger's other trips: %w", err)
	}
	if busy {
		return uuid.Nil, ErrAskNotPending
	}

	// 2. occurrence
	var (
		seatsFree       int
		occurrenceState string
		version         int
		templateDriver  uuid.UUID
	)
	// luggageFree is derived rather than counted: space taken is the sum over the
	// bookings that hold it, so all six paths that cancel a booking give it back
	// by doing what they already do. The FOR UPDATE below is what makes reading
	// the sum and writing the new booking one indivisible thing — two people
	// racing for the last place in the boot serialise on this row exactly as they
	// already do for the last seat.
	var luggageFree int
	var departs time.Time
	var takingOffers bool
	err = tx.QueryRow(ctx, `
		SELECT o.seats_free, o.status, o.itinerary_version, t.driver_id, o.depart_earliest,
		       COALESCE(o.drive_set_at IS NOT NULL AND o.offers_until > $2, false),
		       o.luggage_capacity - COALESCE((
		           SELECT sum(b.luggage_count) FROM bookings b
		           WHERE b.occurrence_id = o.id AND b.status IN ('confirmed', 'completed')
		       ), 0)
		FROM trip_occurrences o
		JOIN route_templates t ON t.id = o.template_id
		WHERE o.id = $1
		FOR UPDATE OF o`, occurrenceID, s.clock.Now()).Scan(&seatsFree, &occurrenceState, &version, &templateDriver, &departs, &takingOffers, &luggageFree)
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: locking occurrence: %w", err)
	}

	// 3. request
	var requestState string
	var passengerID uuid.UUID
	var seats int
	var luggage int
	err = tx.QueryRow(ctx, `
		SELECT status, passenger_id, seats, luggage_count FROM ride_requests WHERE id = $1 FOR UPDATE`,
		requestID).Scan(&requestState, &passengerID, &seats, &luggage)
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: locking request: %w", err)
	}

	// 4. ask
	var (
		askState   string
		expiresAt  time.Time
		askVersion int
	)
	err = tx.QueryRow(ctx, `
		SELECT status, expires_at, itinerary_version FROM asks WHERE id = $1 FOR UPDATE`,
		askID).Scan(&askState, &expiresAt, &askVersion)
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: locking ask: %w", err)
	}

	// Revalidate everything now that the rows are held.
	if templateDriver != driverID {
		return uuid.Nil, ErrNotYours
	}
	if askState == "expired" {
		return uuid.Nil, ErrAskExpired
	}
	if askState != "pending" {
		return uuid.Nil, ErrAskNotPending
	}
	now := s.clock.Now()
	if !now.Before(expiresAt) {
		return uuid.Nil, ErrAskExpired
	}
	if occurrenceState != "scheduled" {
		return uuid.Nil, ErrAskNotPending
	}
	// A departure that has left is not one somebody can be put into, and its
	// status never says so: 'scheduled' is where an occurrence stays for ever
	// after the car has gone, and departed is computed from the clock wherever
	// it is asked. Read after the occurrence is locked, so a departure crossing
	// while this waited on the row is seen rather than missed.
	//
	// Offers now carry deadlines that end at the departure, so the ordinary path
	// cannot produce a pending ask that reaches this line. It is here for the one
	// that does anyway.
	//
	// A drive is the exception: the car has left, but it is still on the road
	// with seats, and the readiness check below decides whether this pickup is
	// still ahead of it.
	if !now.Before(departs) && !takingOffers {
		return uuid.Nil, ErrTripDeparted
	}
	if requestState != "open" && requestState != "asking" {
		return uuid.Nil, ErrAskNotPending
	}
	// On a drive the answer has to come while the passenger can still reach
	// the road before the car: the offer was made against the clock, and the
	// clock has moved while it sat on the driver's phone. Read with both rows
	// held, by the same rule the matcher used to make it — from the car's last
	// trusted fix once it has left, and a car with none is not in time.
	var tooLate bool
	fromAt, fromF := match.DriveFrom("o", "$3::timestamptz")
	if err := tx.QueryRow(ctx, `
		SELECT o.drive_set_at IS NOT NULL AND NOT COALESCE(`+match.DriveReady(fromAt, "o.route_min",
		"(ST_LineLocatePoint(o.route_geom, ST_ClosestPoint(o.route_geom, rr.origin)) - "+fromF+")",
		"ST_Distance(o.route_geom::geography, rr.origin::geography)",
		"GREATEST(rr.window_start, $3::timestamptz)", "rr.window_end", match.DriveLateBy("o."))+`, false)
		  FROM trip_occurrences o `+match.DriveCar("o", "$3")+`, ride_requests rr
		 WHERE o.id = $1 AND rr.id = $2`, occurrenceID, requestID, now).Scan(&tooLate); err != nil {
		return uuid.Nil, fmt.Errorf("asks: checking the pickup is still in time: %w", err)
	}
	if tooLate {
		return uuid.Nil, ErrAskExpired
	}
	var aboard bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM bookings
		               WHERE occurrence_id = $1 AND passenger_id = $2)`,
		occurrenceID, passengerID).Scan(&aboard); err != nil {
		return uuid.Nil, fmt.Errorf("asks: checking for an existing seat: %w", err)
	}
	if aboard {
		return uuid.Nil, ErrAlreadyAboard
	}
	if version != plan.version {
		// The plan moved while the re-quotes were being routed.
		return uuid.Nil, errPlanMoved
	}
	// Quoted against another plan: something else moved it (a drive, a
	// standing arrangement) and this offer was not re-quoted then. It was
	// routed against the plan as it stands before the lock.
	if askVersion != version {
		if plan.self == nil {
			return uuid.Nil, errPlanMoved
		}
		if !plan.self.fits {
			return s.refuse(ctx, tx, askID, requestID, occurrenceID, now, ErrStaleItinerary)
		}
	}
	var parties int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM bookings
		 WHERE occurrence_id = $1 AND status IN ('confirmed', 'completed')`, occurrenceID).Scan(&parties); err != nil {
		return uuid.Nil, fmt.Errorf("asks: counting the groups aboard: %w", err)
	}
	if parties >= match.MaxConcurrentParties {
		return s.refuse(ctx, tx, askID, requestID, occurrenceID, now, match.ErrPartyCapReached)
	}
	if seatsFree < seats {
		return s.refuse(ctx, tx, askID, requestID, occurrenceID, now, ErrNoSeats)
	}
	if luggageFree < luggage {
		return s.refuse(ctx, tx, askID, requestID, occurrenceID, now, ErrNoLuggageRoom)
	}
	if askVersion != version {
		if err := applyQuote(ctx, tx, askID, *plan.self, version); err != nil {
			return uuid.Nil, err
		}
	}

	var bookingID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO bookings (occurrence_id, request_id, passenger_id, ask_id, seats, price_eur,
		                      pricing_basis, pickup_point, dropoff_point, luggage_count)
		SELECT a.occurrence_id, a.request_id, $2, a.id, $3, a.suggested_price_eur,
		       a.pricing_basis, a.pickup_point, a.dropoff_point, $4
		FROM asks a WHERE a.id = $1
		RETURNING id`, askID, passengerID, seats, luggage).Scan(&bookingID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: creating booking: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE trip_occurrences
		SET seats_free = seats_free - $2, itinerary_version = itinerary_version + 1, updated_at = now()
		WHERE id = $1`, occurrenceID, seats); err != nil {
		return uuid.Nil, fmt.Errorf("asks: reserving seats: %w", err)
	}
	if err := s.requoteOthers(ctx, tx, occurrenceID, askID, plan,
		version+1, seatsFree-seats, luggageFree-luggage, now); err != nil {
		return uuid.Nil, err
	}
	if s.afterRequote != nil {
		s.afterRequote()
	}

	if _, err := tx.Exec(ctx,
		`UPDATE asks SET status = 'accepted', responded_at = $2 WHERE id = $1`, askID, now); err != nil {
		return uuid.Nil, fmt.Errorf("asks: marking ask accepted: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE ride_requests SET status = 'matched', updated_at = now() WHERE id = $1`,
		requestID); err != nil {
		return uuid.Nil, fmt.Errorf("asks: closing request: %w", err)
	}

	// The same journey posted for other days too ("Thursday, or else Friday")
	// is carried once: the other days are withdrawn with their offers, and the
	// cars those offers held are free for whoever waits behind them. Safe
	// against two of the days being accepted together: both hold the
	// passenger's lock, taken first above.
	var freed []uuid.UUID
	if err := tx.QueryRow(ctx, `
		WITH me AS (
			SELECT passenger_id, COALESCE(alternative_of, id) AS root FROM ride_requests WHERE id = $1
		), others AS (
			UPDATE ride_requests r SET status = 'cancelled', updated_at = now()
			  FROM me
			 WHERE r.passenger_id = me.passenger_id AND r.id <> $1
			   AND COALESCE(r.alternative_of, r.id) = me.root
			   AND r.status IN ('open', 'asking')
			RETURNING r.id
		), killed AS (
			UPDATE asks SET status = 'cancelled', responded_at = $2
			 WHERE request_id IN (SELECT id FROM others) AND status = 'pending'
			RETURNING occurrence_id
		)
		SELECT ARRAY(SELECT DISTINCT occurrence_id FROM killed)`,
		requestID, now).Scan(&freed); err != nil {
		return uuid.Nil, fmt.Errorf("asks: withdrawing the other days: %w", err)
	}
	if err := outbox.WakeAll(ctx, tx, freed, "alternative_taken", s.clock.Now()); err != nil {
		return uuid.Nil, err
	}

	payload, err := json.Marshal(map[string]any{
		"booking_id":   bookingID,
		"request_id":   requestID,
		"passenger_id": passengerID,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: encoding notification: %w", err)
	}
	if err := outbox.Enqueue(ctx, tx, outbox.Message{
		Kind:           "push",
		IdempotencyKey: "booking_confirmed:" + bookingID.String(),
		Payload:        payload,
	}, s.clock.Now()); err != nil {
		return uuid.Nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("asks: commit: %w", err)
	}
	return bookingID, nil
}

// Decline records a driver's refusal and frees the request for the next
// candidate.
func (s *Service) Decline(ctx context.Context, askID, driverID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("asks: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var requestID, occurrenceID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT request_id, occurrence_id FROM asks WHERE id = $1`, askID).Scan(&requestID, &occurrenceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAskNotFound
	}
	if err != nil {
		return fmt.Errorf("asks: loading ask: %w", err)
	}

	// Canonical lock order, same as Accept.
	var templateDriver uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT t.driver_id FROM trip_occurrences o
		JOIN route_templates t ON t.id = o.template_id
		WHERE o.id = $1 FOR UPDATE OF o`, occurrenceID).Scan(&templateDriver); err != nil {
		return fmt.Errorf("asks: locking occurrence: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM ride_requests WHERE id = $1 FOR UPDATE`, requestID); err != nil {
		return fmt.Errorf("asks: locking request: %w", err)
	}

	var askState string
	var expiresAt time.Time
	if err := tx.QueryRow(ctx,
		`SELECT status, expires_at FROM asks WHERE id = $1 FOR UPDATE`, askID).
		Scan(&askState, &expiresAt); err != nil {
		return fmt.Errorf("asks: locking ask: %w", err)
	}

	if templateDriver != driverID {
		return ErrNotYours
	}
	if askState == "expired" {
		return ErrAskExpired
	}
	if askState != "pending" {
		return ErrAskNotPending
	}
	// The TTL is authoritative. Without this check the outcome would depend on
	// whether the driver or the sweeper got there first after the deadline, so
	// the same late tap could land as either 'declined' or 'expired'.
	if !s.clock.Now().Before(expiresAt) {
		return ErrAskExpired
	}

	if _, err := tx.Exec(ctx,
		`UPDATE asks SET status = 'declined', responded_at = $2 WHERE id = $1`,
		askID, s.clock.Now()); err != nil {
		return fmt.Errorf("asks: marking ask declined: %w", err)
	}
	if err := s.requeue(ctx, tx, requestID, occurrenceID, "declined"); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("asks: commit: %w", err)
	}
	return nil
}

// SweepExpired expires asks whose TTL has passed and queues the next candidate
// for each affected request. It returns how many asks were expired.
func (s *Service) SweepExpired(ctx context.Context) (int, error) {
	now := s.clock.Now()

	rows, err := s.pool.Query(ctx,
		`SELECT id, request_id FROM asks WHERE status = 'pending' AND expires_at <= $1 ORDER BY id`, now)
	if err != nil {
		return 0, fmt.Errorf("asks: finding expired asks: %w", err)
	}
	type expired struct{ askID, requestID uuid.UUID }
	var due []expired
	for rows.Next() {
		var e expired
		if err := rows.Scan(&e.askID, &e.requestID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("asks: scanning expired ask: %w", err)
		}
		due = append(due, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("asks: reading expired asks: %w", err)
	}

	var count int
	for _, e := range due {
		ok, err := s.expireOne(ctx, e.askID, e.requestID, now)
		if err != nil {
			return count, err
		}
		if ok {
			count++
		}
	}
	return count, nil
}

// expireOne expires a single ask, revalidating under the lock so it does not
// race an accept that landed first.
func (s *Service) expireOne(ctx context.Context, askID, requestID uuid.UUID, now time.Time) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("asks: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT 1 FROM ride_requests WHERE id = $1 FOR UPDATE`, requestID); err != nil {
		return false, fmt.Errorf("asks: locking request: %w", err)
	}

	var occurrenceID uuid.UUID
	err = tx.QueryRow(ctx, `
		UPDATE asks SET status = 'expired', responded_at = $2
		WHERE id = $1 AND status = 'pending' AND expires_at <= $2
		RETURNING occurrence_id`, askID, now).Scan(&occurrenceID)
	if errors.Is(err, pgx.ErrNoRows) {
		// An accept or decline won the race; nothing to do.
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("asks: expiring ask: %w", err)
	}

	if err := s.requeue(ctx, tx, requestID, occurrenceID, "expired"); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("asks: commit: %w", err)
	}
	return true, nil
}

// requeue puts a request back in the matching queue via the outbox, so the next
// candidate is approached by the same at-least-once path as everything else.
func (s *Service) requeue(
	ctx context.Context, tx pgx.Tx, requestID, occurrenceID uuid.UUID, reason string,
) error {
	// The offer that just ended was holding a seat against the cap, so the trip
	// has room it did not have a moment ago. Waking it matters most on a
	// one-seat trip, where everybody behind the first offer was turned away:
	// without this the queue never moves, and a trip with a free seat and
	// several people wanting it asks none of them.
	//
	// Queued before the early return below, because the seat comes back whether
	// or not this particular request is still looking.
	if err := outbox.Wake(ctx, tx, occurrenceID, "offer_"+reason, s.clock.Now()); err != nil {
		return err
	}

	tag, err := tx.Exec(ctx,
		`UPDATE ride_requests SET status = 'open', updated_at = now()
		 WHERE id = $1 AND status IN ('open','asking')`, requestID)
	if err != nil {
		return fmt.Errorf("asks: reopening request: %w", err)
	}
	// If the request had already moved on (matched by another ask, cancelled by
	// the passenger), there is nothing to hand to the next driver. Emitting the
	// event regardless would send a second driver after an already-matched
	// passenger.
	if tag.RowsAffected() == 0 {
		return nil
	}

	payload, err := json.Marshal(map[string]any{"request_id": requestID, "reason": reason})
	if err != nil {
		return fmt.Errorf("asks: encoding ask_next: %w", err)
	}
	// The key includes the reason and request so a decline and a later expiry
	// of different asks each get their own message.
	key := fmt.Sprintf("ask_next:%s:%s:%d", requestID, reason, s.clock.Now().UnixNano())
	return outbox.Enqueue(ctx, tx, outbox.Message{
		Kind:           "ask_next",
		IdempotencyKey: key,
		Payload:        payload,
	}, s.clock.Now())
}

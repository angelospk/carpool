package asks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"carpool/internal/geo"
	"carpool/internal/match"
)

// askedBy says who made an ask: the matcher, putting a passenger to a driver,
// or a driver offering a seat to a passenger who is waiting on a weekend plan
// (specs/WEEKEND.md). The values are what asks.offered_by stores.
type askedBy string

const (
	byMatcher askedBy = "matcher"
	byDriver  askedBy = "driver"
)

const (
	// OfferTTL is the longest a driver's offer waits for its passenger.
	OfferTTL = 24 * time.Hour
	// OfferLeadTime is how long before the car leaves an offer ends: a passenger
	// who says yes later than that is not given a seat the driver cannot plan on.
	OfferLeadTime = 2 * time.Hour
	// MinOfferLife is the least a passenger is given to answer. An offer that
	// would end sooner is not made.
	MinOfferLife = 10 * time.Minute
)

var (
	// ErrRequestNotFound means the request is not a weekend request that is
	// still open. Answered as a missing one is, wherever it belongs.
	ErrRequestNotFound = errors.New("asks: no such open weekend request")
	// ErrTooLate means the departure is too near for an offer to live
	// MinOfferLife.
	ErrTooLate = errors.New("asks: the departure is too near to make an offer")
	// ErrCannotCarry means the request does not fit this departure: a block, a
	// full car, a detour the driver does not take, or a passenger who is already
	// going elsewhere at that hour.
	ErrCannotCarry = errors.New("asks: this departure cannot carry that request")
	// ErrAlreadyOffered means this driver has already offered this departure to
	// this request, answered or not.
	ErrAlreadyOffered = errors.New("asks: this departure was already offered to that request")
)

// maxWaiting bounds how many requests are tried against one departure.
const maxWaiting = 50

// Waiting is a weekend request a departure could carry.
type Waiting struct {
	RequestID        uuid.UUID
	PassengerName    string
	Seats            int
	Luggage          string
	LuggageCount     int
	WindowStart      time.Time
	WindowEnd        time.Time
	OriginLabel      string
	DestinationLabel string
}

// ownsDeparture is nil when the departure is this driver's. Somebody else's
// departure and one that does not exist are the same answer: ErrNotYours.
func (s *Service) ownsDeparture(ctx context.Context, occurrenceID, driverID uuid.UUID) error {
	var owner uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT t.driver_id FROM trip_occurrences o JOIN route_templates t ON t.id = o.template_id
		WHERE o.id = $1`, occurrenceID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != driverID) {
		return ErrNotYours
	}
	if err != nil {
		return fmt.Errorf("asks: loading the departure: %w", err)
	}
	return nil
}

// Waiting lists the open weekend requests this departure could carry: those the
// gross filter lets through with the occurrence fixed, which is the same
// question the matcher asks of an ordinary request.
func (s *Service) Waiting(ctx context.Context, occurrenceID, driverID uuid.UUID) ([]Waiting, error) {
	if err := s.ownsDeparture(ctx, occurrenceID, driverID); err != nil {
		return nil, err
	}

	// Cheap and conservative first, so the filter is run for the requests this
	// departure could possibly meet and not for every weekend request there is:
	// open, not spoken for, hours that meet the departure's, and both ends
	// within the driver's radius of the road, as the filter itself requires.
	rows, err := s.pool.Query(ctx, `
		SELECT rr.id
		  FROM trip_occurrences o
		  JOIN route_templates t ON t.id = o.template_id
		  JOIN driver_profiles d ON d.user_id = t.driver_id
		  JOIN ride_requests rr ON rr.weekend_plan_id IS NOT NULL AND rr.status = 'open'
		  JOIN users u ON u.id = rr.passenger_id AND u.status = 'active'
		 WHERE o.id = $1
		   AND rr.window_start <= o.depart_latest AND rr.window_end >= o.depart_earliest
		   AND rr.passenger_id <> t.driver_id
		   AND NOT EXISTS (SELECT 1 FROM asks a WHERE a.request_id = rr.id AND a.status = 'pending')
		   AND ST_DWithin(o.route_geom::geography, rr.origin::geography, d.notify_radius_km * 1000)
		   AND ST_DWithin(o.route_geom::geography, rr.destination::geography, d.notify_radius_km * 1000)
		 ORDER BY rr.window_start, rr.id
		 LIMIT $2`, occurrenceID, maxWaiting*4)
	if err != nil {
		return nil, fmt.Errorf("asks: finding weekend requests: %w", err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("asks: reading weekend requests: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("asks: reading weekend requests: %w", err)
	}

	out := []Waiting{}
	for _, id := range ids {
		if len(out) == maxWaiting {
			break
		}
		req, err := s.loadRequest(ctx, id)
		if errors.Is(err, ErrNoCandidates) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !req.Weekend || req.Status != "open" || req.PassengerStatus != "active" {
			continue
		}
		q := s.grossQuery(req)
		q.Target = &occurrenceID
		candidates, err := match.Gross(ctx, s.pool, q)
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			continue
		}

		w := Waiting{RequestID: id, Seats: req.Seats, Luggage: string(req.Luggage), LuggageCount: req.LuggageCount,
			WindowStart: req.WindowStart, WindowEnd: req.WindowEnd}
		if err := s.pool.QueryRow(ctx, `
			SELECT u.name, coalesce(rr.origin_label, ''), coalesce(rr.destination_label, '')
			  FROM ride_requests rr JOIN users u ON u.id = rr.passenger_id
			 WHERE rr.id = $1`, id).Scan(&w.PassengerName, &w.OriginLabel, &w.DestinationLabel); err != nil {
			return nil, fmt.Errorf("asks: reading a waiting request: %w", err)
		}
		out = append(out, w)
	}
	return out, nil
}

// Offer makes an ask on this departure for a waiting weekend request, as a
// driver's offer: quoted as the matcher quotes one, under the same locks and
// checks, and ending two hours before the car leaves or in a day, whichever is
// sooner.
func (s *Service) Offer(ctx context.Context, occurrenceID, driverID, requestID uuid.UUID) (uuid.UUID, error) {
	if err := s.ownsDeparture(ctx, occurrenceID, driverID); err != nil {
		return uuid.Nil, err
	}
	req, err := s.loadRequest(ctx, requestID)
	if errors.Is(err, ErrNoCandidates) {
		return uuid.Nil, ErrRequestNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	if !req.Weekend || req.Status != "open" || req.PassengerStatus != "active" {
		return uuid.Nil, ErrRequestNotFound
	}

	q := s.grossQuery(req)
	q.Target = &occurrenceID
	candidates, err := match.Gross(ctx, s.pool, q)
	if err != nil {
		return uuid.Nil, err
	}
	if len(candidates) == 0 {
		return uuid.Nil, ErrCannotCarry
	}
	c := candidates[0]

	insertion, price, basis, err := s.evaluate(ctx, c, req)
	if errors.Is(err, geo.ErrNoRoute) || errors.Is(err, match.ErrNoViableInsertion) {
		return uuid.Nil, ErrCannotCarry
	}
	if err != nil {
		return uuid.Nil, err
	}

	askID, err := s.createAsk(ctx, req, c, insertion, price, basis, byDriver)
	switch {
	case errors.Is(err, errAskExists):
		return uuid.Nil, ErrAlreadyOffered
	case errors.Is(err, errNoRoomToAsk):
		return uuid.Nil, ErrCannotCarry
	case err != nil:
		return uuid.Nil, err
	}
	return askID, nil
}

// offeredTo is the driver an offer is with, provided the offer is the
// passenger's. Somebody else's and no such offer are one answer.
func (s *Service) offeredTo(ctx context.Context, askID, passengerID uuid.UUID) (uuid.UUID, error) {
	var driverID, owner uuid.UUID
	var by string
	err := s.pool.QueryRow(ctx, `
		SELECT t.driver_id, rr.passenger_id, a.offered_by
		  FROM asks a
		  JOIN ride_requests rr   ON rr.id = a.request_id
		  JOIN trip_occurrences o ON o.id = a.occurrence_id
		  JOIN route_templates t  ON t.id = o.template_id
		 WHERE a.id = $1`, askID).Scan(&driverID, &owner, &by)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrAskNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: loading offer: %w", err)
	}
	if owner != passengerID || by != string(byDriver) {
		return uuid.Nil, ErrNotYours
	}
	return driverID, nil
}

// AcceptOffer books the seat a driver offered, for the passenger it was
// offered to. It is Accept, with every lock and check it makes.
func (s *Service) AcceptOffer(ctx context.Context, askID, passengerID uuid.UUID) (uuid.UUID, error) {
	driverID, err := s.offeredTo(ctx, askID, passengerID)
	if err != nil {
		return uuid.Nil, err
	}
	return s.acceptAsked(ctx, askID, driverID, byDriver)
}

// DeclineOffer ends a driver's offer at the passenger's word. The request is
// open again for another driver to offer.
func (s *Service) DeclineOffer(ctx context.Context, askID, passengerID uuid.UUID) error {
	if _, err := s.offeredTo(ctx, askID, passengerID); err != nil {
		return err
	}
	return s.decline(ctx, askID, passengerID, byDriver)
}

var athens = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Athens")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// dayPhrase is a departure's day as it is read after «σε πάρει»: την Παρασκευή,
// το Σάββατο.
func dayPhrase(t time.Time) string {
	switch t.In(athens).Weekday() {
	case time.Monday:
		return "τη Δευτέρα"
	case time.Tuesday:
		return "την Τρίτη"
	case time.Wednesday:
		return "την Τετάρτη"
	case time.Thursday:
		return "την Πέμπτη"
	case time.Friday:
		return "την Παρασκευή"
	case time.Saturday:
		return "το Σάββατο"
	default:
		return "την Κυριακή"
	}
}

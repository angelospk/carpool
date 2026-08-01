package asks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrNothingToRepeat means there is no future trip on that route to return to,
// or the booking is not the caller's to repeat.
var ErrNothingToRepeat = errors.New("asks: nothing to repeat")

// Again asks the same driver for the same ride, on the next running of their
// route.
//
// It goes through the ordinary request and ask machinery rather than writing a
// booking: the driver agreed to one journey, not to a standing claim, and the
// detour, the price and the seat all have to be settled again for the day in
// question. What changes is only that the request names an occurrence, so the
// matcher approaches that driver instead of ranking the field.
func (s *Service) Again(
	ctx context.Context, bookingID, passengerID uuid.UUID,
) (requestID, askID uuid.UUID, err error) {
	now := s.clock.Now()

	var (
		owner      uuid.UUID
		templateID uuid.UUID
		seats      int
	)
	err = s.pool.QueryRow(ctx, `
		SELECT b.passenger_id, o.template_id, b.seats
		FROM bookings b
		JOIN trip_occurrences o ON o.id = b.occurrence_id
		WHERE b.id = $1`, bookingID).Scan(&owner, &templateID, &seats)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, uuid.Nil, ErrNothingToRepeat
	}
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("asks: loading the ride to repeat: %w", err)
	}
	if owner != passengerID {
		return uuid.Nil, uuid.Nil, ErrNotYours
	}

	// The next running of that route the passenger is not already on. A trip
	// they have already booked is not a trip to ask about.
	var (
		target                 uuid.UUID
		departEarliest, latest time.Time
	)
	err = s.pool.QueryRow(ctx, `
		SELECT o.id, o.depart_earliest, o.depart_latest
		FROM trip_occurrences o
		WHERE o.template_id = $1
		  AND o.status = 'scheduled'
		  AND o.depart_earliest > $2
		  AND o.seats_free >= $3
		  AND NOT EXISTS (SELECT 1 FROM bookings b
		                  WHERE b.occurrence_id = o.id AND b.passenger_id = $4)
		ORDER BY o.depart_earliest
		LIMIT 1`, templateID, now, seats, passengerID).Scan(&target, &departEarliest, &latest)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, uuid.Nil, ErrNothingToRepeat
	}
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("asks: finding the next running: %w", err)
	}

	// The window brackets that departure rather than being asked of the
	// passenger: they tapped a trip, not a time.
	err = s.pool.QueryRow(ctx, `
		INSERT INTO ride_requests
		(passenger_id, origin, destination, origin_label, destination_label,
		 window_start, window_end, seats, kind, target_occurrence_id, status)
		SELECT b.passenger_id, b.pickup_point, b.dropoff_point,
		       t.origin_label, t.destination_label,
		       $2, $3, b.seats, 'targeted', $4, 'open'
		FROM bookings b
		JOIN trip_occurrences o ON o.id = b.occurrence_id
		JOIN route_templates  t ON t.id = o.template_id
		WHERE b.id = $1
		RETURNING id`,
		bookingID, departEarliest.Add(-time.Hour), latest.Add(time.Hour), target).Scan(&requestID)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("asks: recording the repeat request: %w", err)
	}

	askID, err = s.AskNext(ctx, requestID)
	if err != nil {
		// No ask means that day did not work out — the detour no longer fits,
		// or the seat went while this was being decided. The request is left
		// open rather than deleted: it is a real thing the passenger asked for,
		// and the sweep expires it in its own time.
		return requestID, uuid.Nil, err
	}
	return requestID, askID, nil
}

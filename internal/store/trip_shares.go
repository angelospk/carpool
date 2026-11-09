package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNoSuchSharedTrip is a link that names nothing: never made, withdrawn, or
// its passenger no longer on the trip.
var ErrNoSuchSharedTrip = errors.New("store: no such shared trip")

// ErrNotAPassenger is somebody on the trip who is not riding in it: the
// driver.
var ErrNotAPassenger = errors.New("store: only a passenger shares a trip")

// SharedTrip is what a trip link says. The passenger is kept so the live
// position can be read as them: the link shows nothing they could not see.
type SharedTrip struct {
	OccurrenceID     uuid.UUID
	PassengerID      uuid.UUID
	DriverID         uuid.UUID
	OriginLabel      string
	DestinationLabel string
	DepartAt         time.Time
	DriverName       string
	CarModel         string
	CarColor         string
	CarPlate         string
}

// ShareTrip returns the passenger's link to the trip, making it the first
// time. The driver gets ErrNotAPassenger, anybody else ErrNoSuchSharedTrip.
func ShareTrip(ctx context.Context, pool *pgxpool.Pool, occurrenceID, userID uuid.UUID) (string, error) {
	var isDriver, hasSeat bool
	err := pool.QueryRow(ctx, `
		SELECT t.driver_id = $2,
		       EXISTS (SELECT 1 FROM bookings b WHERE b.occurrence_id = o.id
		               AND b.passenger_id = $2 AND b.status = 'confirmed')
		FROM trip_occurrences o JOIN route_templates t ON t.id = o.template_id
		WHERE o.id = $1`, occurrenceID, userID).Scan(&isDriver, &hasSeat)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoSuchSharedTrip
	}
	if err != nil {
		return "", fmt.Errorf("store: checking the trip: %w", err)
	}
	if isDriver {
		return "", ErrNotAPassenger
	}
	if !hasSeat {
		return "", ErrNoSuchSharedTrip
	}
	fresh, err := newShareToken()
	if err != nil {
		return "", err
	}
	var token string
	// The no-op update makes RETURNING give the existing row on a conflict.
	err = pool.QueryRow(ctx, `
		INSERT INTO trip_shares (token, occurrence_id, passenger_id) VALUES ($3, $1, $2)
		ON CONFLICT (occurrence_id, passenger_id) DO UPDATE SET token = trip_shares.token
		RETURNING token`, occurrenceID, userID, fresh).Scan(&token)
	if err != nil {
		return "", fmt.Errorf("store: sharing the trip: %w", err)
	}
	return token, nil
}

// RevokeTripShare withdraws the passenger's link. No link is not an error:
// the result is the same.
func RevokeTripShare(ctx context.Context, pool *pgxpool.Pool, occurrenceID, userID uuid.UUID) error {
	if _, err := pool.Exec(ctx, `DELETE FROM trip_shares WHERE occurrence_id = $1 AND passenger_id = $2`,
		occurrenceID, userID); err != nil {
		return fmt.Errorf("store: withdrawing the trip link: %w", err)
	}
	return nil
}

// SharedTripByToken reads what a link says. A link whose passenger no longer
// holds a confirmed seat, or whose trip was called off, names nothing.
func SharedTripByToken(ctx context.Context, pool *pgxpool.Pool, token string) (SharedTrip, error) {
	var s SharedTrip
	var model, color, plate *string
	err := pool.QueryRow(ctx, `
		SELECT o.id, ts.passenger_id, t.driver_id, t.origin_label, t.destination_label, o.depart_earliest,
		       u.name, dp.car_model, dp.car_color, dp.car_plate
		FROM trip_shares ts
		JOIN trip_occurrences o ON o.id = ts.occurrence_id
		JOIN route_templates t ON t.id = o.template_id
		JOIN users u ON u.id = t.driver_id
		LEFT JOIN driver_profiles dp ON dp.user_id = t.driver_id
		WHERE ts.token = $1 AND o.status <> 'cancelled'
		  AND EXISTS (SELECT 1 FROM bookings b WHERE b.occurrence_id = o.id
		              AND b.passenger_id = ts.passenger_id AND b.status = 'confirmed')`, token).
		Scan(&s.OccurrenceID, &s.PassengerID, &s.DriverID, &s.OriginLabel, &s.DestinationLabel, &s.DepartAt,
			&s.DriverName, &model, &color, &plate)
	if errors.Is(err, pgx.ErrNoRows) {
		return SharedTrip{}, ErrNoSuchSharedTrip
	}
	if err != nil {
		return SharedTrip{}, fmt.Errorf("store: reading the trip link: %w", err)
	}
	for dst, src := range map[*string]*string{&s.CarModel: model, &s.CarColor: color, &s.CarPlate: plate} {
		if src != nil {
			*dst = *src
		}
	}
	return s, nil
}

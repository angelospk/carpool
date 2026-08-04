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

// TripRole is which side of a trip a user is on.
type TripRole string

const (
	RoleDriver    TripRole = "driver"
	RolePassenger TripRole = "passenger"
)

// Trip is one occurrence as it appears to a participant.
type Trip struct {
	OccurrenceID   uuid.UUID
	Role           TripRole
	DepartEarliest time.Time
	DepartLatest   time.Time
	Status         string
	RouteKm        float64
	OriginLabel    string
	DestLabel      string
	PriceEUR       float64
	SeatsFree      int
}

// UpcomingTrips lists a user's future trips, driving and riding merged, which
// is what the app's home screen shows.
func UpcomingTrips(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, now time.Time) ([]Trip, error) {
	rows, err := pool.Query(ctx, `
		SELECT o.id, 'driver', o.depart_earliest, o.depart_latest, o.status, o.route_km,
		       t.origin_label, t.destination_label, 0::numeric, o.seats_free
		FROM trip_occurrences o
		JOIN route_templates t ON t.id = o.template_id
		WHERE t.driver_id = $1
		  AND o.status IN ('scheduled','started')
		  AND o.depart_latest >= $2

		UNION ALL

		SELECT o.id, 'passenger', o.depart_earliest, o.depart_latest, o.status, o.route_km,
		       t.origin_label, t.destination_label, b.price_eur, o.seats_free
		FROM bookings b
		JOIN trip_occurrences o ON o.id = b.occurrence_id
		JOIN route_templates t  ON t.id = o.template_id
		WHERE b.passenger_id = $1
		  AND b.status = 'confirmed'
		  AND o.status IN ('scheduled','started')
		  AND o.depart_latest >= $2

		ORDER BY 3`, userID, now)
	if err != nil {
		return nil, fmt.Errorf("store: listing upcoming trips: %w", err)
	}
	defer rows.Close()

	var out []Trip
	for rows.Next() {
		var t Trip
		if err := rows.Scan(&t.OccurrenceID, &t.Role, &t.DepartEarliest, &t.DepartLatest,
			&t.Status, &t.RouteKm, &t.OriginLabel, &t.DestLabel, &t.PriceEUR, &t.SeatsFree); err != nil {
			return nil, fmt.Errorf("store: scanning trip: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TripParticipant is a person on a trip.
type TripParticipant struct {
	UserID uuid.UUID
	Name   string
	// PhotoVersion is a prefix of the stored photo's hash, absent when there is
	// none. The legacy photo_url column is deliberately not carried: it holds
	// URLs on origins the user chose, from before uploads existed, and handing
	// one to every other user's device is what the upload path exists to stop.
	PhotoVersion *string
	Rating       *float64
	// The handles are where this person says they can also be found. They travel
	// only inside a trip both parties are on, for the same reason the plate
	// does: it is theirs to hand to someone they are riding with, not to the
	// whole region.
	FacebookHandle  *string
	InstagramHandle *string
	// Driver-only fields.
	CarModel *string
	CarColor *string
	CarPlate *string
	// Passenger-only fields.
	PriceEUR *float64
	Seats    *int
}

// TripDetail is the trip card.
type TripDetail struct {
	Trip
	Driver     TripParticipant
	Passengers []TripParticipant
	// RoutePolyline is the stored geometry encoded for the app's map.
	RoutePolyline string
	// TemplateID and Weekday let the app tell whether a standing arrangement
	// already covers this weekday of this route.
	TemplateID uuid.UUID
	Weekday    int
	// BookingID is the caller's own booking, and the thing a repeat is founded
	// on. It is empty for the driver, who has no booking of their own.
	BookingID *uuid.UUID
}

// ErrNotParticipant means the caller has no place on this trip. It is reported
// as a 404 rather than a 403 so trip ids cannot be probed.
var ErrNotParticipant = errors.New("store: not a participant of this trip")

// GetTrip returns the trip card, provided the caller is on the trip.
//
// The driver's plate is included: SPEC.md withholds it until a booking exists,
// and reaching this function already required one.
func GetTrip(ctx context.Context, pool *pgxpool.Pool, occurrenceID, userID uuid.UUID) (TripDetail, error) {
	role, err := ParticipantRole(ctx, pool, occurrenceID, userID)
	if err != nil {
		return TripDetail{}, err
	}

	var d TripDetail
	d.Role = role
	d.OccurrenceID = occurrenceID

	err = pool.QueryRow(ctx, `
		SELECT o.depart_earliest, o.depart_latest, o.status, o.route_km, o.seats_free,
		       t.origin_label, t.destination_label,
		       o.template_id, EXTRACT(ISODOW FROM o.service_date)::int,
		       (SELECT b.id FROM bookings b
		        WHERE b.occurrence_id = o.id AND b.passenger_id = $2
		          AND b.status IN ('confirmed','completed')),
		       ST_AsEncodedPolyline(o.route_geom, 6),
		       u.id, u.name,
		       (SELECT encode(substring(sha256 from 1 for 6), 'hex') FROM profile_photos WHERE user_id = u.id),
		       u.facebook_handle, u.instagram_handle,
		       (SELECT round(avg(stars)::numeric, 2) FROM ratings WHERE ratee_id = u.id),
		       dp.car_model, dp.car_color, dp.car_plate
		FROM trip_occurrences o
		JOIN route_templates t  ON t.id = o.template_id
		JOIN driver_profiles dp ON dp.user_id = t.driver_id
		JOIN users u            ON u.id = t.driver_id
		WHERE o.id = $1`, occurrenceID, userID).Scan(
		&d.DepartEarliest, &d.DepartLatest, &d.Status, &d.RouteKm, &d.SeatsFree,
		&d.OriginLabel, &d.DestLabel, &d.TemplateID, &d.Weekday, &d.BookingID, &d.RoutePolyline,
		&d.Driver.UserID, &d.Driver.Name, &d.Driver.PhotoVersion, &d.Driver.FacebookHandle, &d.Driver.InstagramHandle, &d.Driver.Rating,
		&d.Driver.CarModel, &d.Driver.CarColor, &d.Driver.CarPlate)
	if errors.Is(err, pgx.ErrNoRows) {
		return TripDetail{}, ErrNotParticipant
	}
	if err != nil {
		return TripDetail{}, fmt.Errorf("store: loading trip: %w", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT u.id, u.name,
		       (SELECT encode(substring(sha256 from 1 for 6), 'hex') FROM profile_photos WHERE user_id = u.id),
		       u.facebook_handle, u.instagram_handle,
		       (SELECT round(avg(stars)::numeric, 2) FROM ratings WHERE ratee_id = u.id),
		       b.price_eur, b.seats
		FROM bookings b JOIN users u ON u.id = b.passenger_id
		WHERE b.occurrence_id = $1 AND b.status IN ('confirmed','completed')
		ORDER BY b.created_at`, occurrenceID)
	if err != nil {
		return TripDetail{}, fmt.Errorf("store: loading passengers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p TripParticipant
		if err := rows.Scan(&p.UserID, &p.Name, &p.PhotoVersion, &p.FacebookHandle, &p.InstagramHandle, &p.Rating,
			&p.PriceEUR, &p.Seats); err != nil {
			return TripDetail{}, fmt.Errorf("store: scanning passenger: %w", err)
		}
		if p.UserID == userID && p.PriceEUR != nil {
			d.PriceEUR = *p.PriceEUR
		}
		d.Passengers = append(d.Passengers, p)
	}
	return d, rows.Err()
}

// ParticipantRole reports how a user is attached to an occurrence, and is the
// single authorization gate for every trip-scoped endpoint.
func ParticipantRole(ctx context.Context, pool *pgxpool.Pool, occurrenceID, userID uuid.UUID) (TripRole, error) {
	var role *string
	err := pool.QueryRow(ctx, `
		SELECT CASE
		         WHEN t.driver_id = $2 THEN 'driver'
		         WHEN EXISTS (SELECT 1 FROM bookings b
		                      WHERE b.occurrence_id = o.id AND b.passenger_id = $2
		                        AND b.status IN ('confirmed','completed')) THEN 'passenger'
		       END
		FROM trip_occurrences o
		JOIN route_templates t ON t.id = o.template_id
		WHERE o.id = $1`, occurrenceID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotParticipant
	}
	if err != nil {
		return "", fmt.Errorf("store: checking participation: %w", err)
	}
	if role == nil {
		return "", ErrNotParticipant
	}
	return TripRole(*role), nil
}

// ConfirmTrip records that one side says the ride happened, and completes the
// bookings where both sides now agree.
//
// It is idempotent: confirming twice is the same as confirming once, which
// matters because the app retries on a flaky connection.
func ConfirmTrip(ctx context.Context, pool *pgxpool.Pool, occurrenceID, userID uuid.UUID, role TripRole, now time.Time) error {
	var sql string
	switch role {
	case RoleDriver:
		// The driver confirms every passenger at once. The driver identity is
		// re-checked in SQL rather than trusted from the role argument, so a
		// mis-derived role upstream cannot confirm other people's bookings.
		sql = `UPDATE bookings SET driver_confirmed = true, updated_at = now()
		       WHERE occurrence_id = $1 AND status = 'confirmed'
		         AND $2 = (SELECT t.driver_id FROM trip_occurrences o
		                   JOIN route_templates t ON t.id = o.template_id
		                   WHERE o.id = $1)`
	case RolePassenger:
		sql = `UPDATE bookings SET passenger_confirmed = true, updated_at = now()
		       WHERE occurrence_id = $1 AND passenger_id = $2 AND status = 'confirmed'`
	default:
		return ErrNotParticipant
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// A ride can only be confirmed after it was due to leave. Without this a
	// pair could confirm and then rate a trip scheduled for next week.
	var departed bool
	err = tx.QueryRow(ctx,
		`SELECT depart_earliest <= $2 FROM trip_occurrences WHERE id = $1 FOR UPDATE`,
		occurrenceID, now).Scan(&departed)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotParticipant
	}
	if err != nil {
		return fmt.Errorf("store: loading occurrence: %w", err)
	}
	if !departed {
		return ErrNotDeparted
	}

	if _, err := tx.Exec(ctx, sql, occurrenceID, userID); err != nil {
		return fmt.Errorf("store: recording confirmation: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE bookings SET status = 'completed', updated_at = now()
		WHERE occurrence_id = $1 AND status = 'confirmed'
		  AND driver_confirmed AND passenger_confirmed`, occurrenceID); err != nil {
		return fmt.Errorf("store: completing bookings: %w", err)
	}
	// Once no confirmed booking is left, the trip itself is done.
	if _, err := tx.Exec(ctx, `
		UPDATE trip_occurrences SET status = 'completed', updated_at = now()
		WHERE id = $1 AND status IN ('scheduled','started')
		  AND NOT EXISTS (SELECT 1 FROM bookings WHERE occurrence_id = $1 AND status = 'confirmed')
		  AND EXISTS (SELECT 1 FROM bookings WHERE occurrence_id = $1 AND status = 'completed')`,
		occurrenceID); err != nil {
		return fmt.Errorf("store: completing occurrence: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// Errors the rating flow can return.
var (
	// ErrNotCompleted means the ride has not been confirmed by both sides yet.
	ErrNotCompleted = errors.New("store: the ride is not completed yet")
	// ErrAlreadyRated means this rater already rated this booking.
	ErrAlreadyRated = errors.New("store: already rated")
	// ErrNotDeparted means the trip has not left yet.
	ErrNotDeparted = errors.New("store: the trip has not departed yet")
)

// RateTrip stores a rating from one participant about the other.
func RateTrip(ctx context.Context, pool *pgxpool.Pool, occurrenceID, raterID uuid.UUID,
	role TripRole, rateeID *uuid.UUID, stars int, tag *string,
) error {
	// The counterparty depends on which side is rating: a passenger rates the
	// driver, the driver rates that passenger.
	var bookingID, target uuid.UUID
	var query string
	switch role {
	case RolePassenger:
		// A passenger has exactly one counterparty: the driver.
		query = `SELECT b.id, t.driver_id
		         FROM bookings b
		         JOIN trip_occurrences o ON o.id = b.occurrence_id
		         JOIN route_templates t  ON t.id = o.template_id
		         WHERE b.occurrence_id = $1 AND b.passenger_id = $2 AND b.status = 'completed'
		           AND ($3::uuid IS NULL OR $3 = t.driver_id)`
	case RoleDriver:
		// A driver may be carrying several passengers, so the rating has to name
		// which one. Picking the oldest booking would make everyone after the
		// first unratable.
		query = `SELECT b.id, b.passenger_id
		         FROM bookings b
		         WHERE b.occurrence_id = $1 AND b.status = 'completed'
		           AND $2 = (SELECT t.driver_id FROM trip_occurrences o
		                     JOIN route_templates t ON t.id = o.template_id WHERE o.id = $1)
		           AND ($3::uuid IS NULL OR b.passenger_id = $3)
		         ORDER BY b.created_at, b.id LIMIT 1`
	default:
		return ErrNotParticipant
	}

	err := pool.QueryRow(ctx, query, occurrenceID, raterID, rateeID).Scan(&bookingID, &target)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotCompleted
	}
	if err != nil {
		return fmt.Errorf("store: loading booking to rate: %w", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO ratings (booking_id, rater_id, ratee_id, stars, tag)
		VALUES ($1, $2, $3, $4, $5)`, bookingID, raterID, target, stars, tag)
	if isUniqueViolation(err) {
		return ErrAlreadyRated
	}
	if err != nil {
		return fmt.Errorf("store: storing rating: %w", err)
	}
	return nil
}

// PastTrip is a ride that has already happened, as the passenger sees it in
// their history. It carries the booking id because that is what a repeat is
// founded on: the stops, the seats and the price were settled there.
type PastTrip struct {
	Trip
	BookingID  uuid.UUID
	DriverName string
}

// PastTrips lists a passenger's completed rides, newest first.
//
// Only the passenger side: a driver repeating their own trip is what the
// schedule is for, and a route template already says "every Tuesday".
func PastTrips(
	ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, now time.Time, limit int,
) ([]PastTrip, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := pool.Query(ctx, `
		SELECT o.id, o.depart_earliest, o.depart_latest, o.status, o.route_km,
		       t.origin_label, t.destination_label, b.price_eur, o.seats_free,
		       b.id, u.name
		FROM bookings b
		JOIN trip_occurrences o ON o.id = b.occurrence_id
		JOIN route_templates t  ON t.id = o.template_id
		JOIN users u            ON u.id = t.driver_id
		WHERE b.passenger_id = $1
		  AND b.status IN ('confirmed','completed')
		  AND o.depart_latest < $2
		ORDER BY o.depart_earliest DESC
		LIMIT $3`, userID, now, limit)
	if err != nil {
		return nil, fmt.Errorf("store: loading past trips: %w", err)
	}
	defer rows.Close()

	var out []PastTrip
	for rows.Next() {
		var p PastTrip
		p.Role = RolePassenger
		if err := rows.Scan(&p.OccurrenceID, &p.DepartEarliest, &p.DepartLatest, &p.Status,
			&p.RouteKm, &p.OriginLabel, &p.DestLabel, &p.PriceEUR, &p.SeatsFree,
			&p.BookingID, &p.DriverName); err != nil {
			return nil, fmt.Errorf("store: scanning a past trip: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

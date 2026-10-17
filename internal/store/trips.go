package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/domain"
	"carpool/internal/match"
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
	// Seats is how many seats the caller's own booking holds, so a per-seat
	// price can be shown with its total. Nil for the driver.
	Seats *int
	// Repeats is whether the route runs again: its schedule is recurring.
	Repeats bool
}

// tripEndsAt is when the departure o is over, for listing it as next or as
// past: when it arrives (the latest departure plus the route's minutes), or
// for a drive when the driver taps «Τέλος». A drive nobody ended is given three
// hours past its arrival, so a forgotten one does not stay on the home screen.
//
// Listing by the departure window instead dropped a trip from the driver's
// home, and moved it into the passenger's past trips, while the car was still
// on the road (QA 2026-10-17 U F1).
func tripEndsAt(o string) string {
	arrives := o + ".depart_latest + make_interval(mins => " + o + ".route_min)"
	return "(CASE WHEN " + o + ".drive_ended_at IS NOT NULL THEN " + o + ".drive_ended_at" +
		" WHEN " + o + ".drive_set_at IS NOT NULL THEN " + arrives + " + interval '3 hours'" +
		" ELSE " + arrives + " END)"
}

// UpcomingTrips lists a user's future trips, driving and riding merged, which
// is what the app's home screen shows.
func UpcomingTrips(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, now time.Time) ([]Trip, error) {
	rows, err := pool.Query(ctx, `
		SELECT o.id, 'driver', o.depart_earliest, o.depart_latest, o.status, o.route_km,
		       t.origin_label, t.destination_label, 0::numeric, o.seats_free, NULL::int
		FROM trip_occurrences o
		JOIN route_templates t ON t.id = o.template_id
		WHERE t.driver_id = $1
		  -- Completed too: both sides may confirm once the car has left, which
		  -- completes the occurrence while it is still on the road. Time, not
		  -- status, says when a trip is over.
		  AND o.status IN ('scheduled','started','completed')
		  AND `+tripEndsAt("o")+` >= $2

		UNION ALL

		SELECT o.id, 'passenger', o.depart_earliest, o.depart_latest, o.status, o.route_km,
		       t.origin_label, t.destination_label, b.price_eur, o.seats_free, b.seats
		FROM bookings b
		JOIN trip_occurrences o ON o.id = b.occurrence_id
		JOIN route_templates t  ON t.id = o.template_id
		WHERE b.passenger_id = $1
		  -- Completed too: confirming is allowed once the car has left, and a
		  -- ride confirmed on the way is still on the road.
		  AND b.status IN ('confirmed','completed')
		  AND o.status IN ('scheduled','started','completed')
		  AND `+tripEndsAt("o")+` >= $2

		ORDER BY 3`, userID, now)
	if err != nil {
		return nil, fmt.Errorf("store: listing upcoming trips: %w", err)
	}
	defer rows.Close()

	var out []Trip
	for rows.Next() {
		var t Trip
		if err := rows.Scan(&t.OccurrenceID, &t.Role, &t.DepartEarliest, &t.DepartLatest,
			&t.Status, &t.RouteKm, &t.OriginLabel, &t.DestLabel, &t.PriceEUR, &t.SeatsFree, &t.Seats); err != nil {
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
	// Bio and Preferences say what the ride is like, which the photo and the
	// rating do not. They travel with the trip because that is where somebody
	// is deciding whether to get in.
	Bio         *string
	Preferences []string
	// The handles are where this person says they can also be found. They travel
	// only inside a trip both parties are on, for the same reason the plate
	// does: it is theirs to hand to someone they are riding with, not to the
	// whole region.
	FacebookHandle  *string
	InstagramHandle *string
	// Phone is the number to call, and is nil unless the caller and this person
	// are in a confirmed booking together and it is the day of the trip: see
	// CallWindow. Co-passengers never get each other's.
	Phone *string
	// CallUntil is when the window closes, set exactly when Phone is, so the
	// app can stop offering the call without another fetch.
	CallUntil *time.Time
	// Driver-only fields.
	CarModel *string
	CarColor *string
	CarPlate *string
	// Passenger-only fields.
	PriceEUR *float64
	Seats    *int
	// CanRate and Rated are relative to whoever is looking. A driver rates each
	// passenger whose booking completed; a passenger rates only the driver.
	CanRate bool
	Rated   bool
	// ArrivedAt is the passenger's «Είμαι εδώ» on a drive.
	ArrivedAt *time.Time
	// BookingID is the seat, for the driver: it is what an accepted offer
	// returns, and how the app finds the pickup to hand to a maps app.
	BookingID *uuid.UUID
	// Pickup is where the passenger gets in, as [lon, lat]: for the driver,
	// who hands it to their maps app, and for the passenger themselves.
	// Nobody else on the trip is told where another passenger waits.
	Pickup *[2]float64
	// Dropoff is where they get out, [lon, lat], on the same terms: the
	// driver's maps app has to stop there too.
	Dropoff *[2]float64
}

// TripDetail is the trip card.
type TripDetail struct {
	Trip
	// SeatsTotal is the car's size for this departure, so the app can draw
	// it whole when a passenger is not shown the other bookings' seats.
	SeatsTotal int
	Driver     TripParticipant
	Passengers []TripParticipant
	// RoutePolyline is the stored geometry encoded for the app's map.
	RoutePolyline string
	// TemplateID and Weekday let the app tell whether a standing arrangement
	// already covers this weekday of this route.
	TemplateID uuid.UUID
	Weekday    int
	// Basis is the frozen receipt for the caller's own seat: why that price was
	// that price. Nil for the driver, and for anybody whose booking predates
	// the column.
	Basis *domain.Basis
	// BookingID is the caller's own booking, and the thing a repeat is founded
	// on. It is empty for the driver, who has no booking of their own.
	BookingID *uuid.UUID
	// RouteMin is how long the road takes, which with the departure window is
	// when live sharing and «Είμαι εδώ» are open.
	RouteMin int
	// EndsAtPort marks a trip the router cut short at a ferry terminal, so the
	// app can explain why it stops somewhere nobody typed.
	EndsAtPort bool
	// ConfirmedByMe is whether the caller has said the ride happened: their own
	// booking for a passenger, their passengers' bookings for the driver.
	ConfirmedByMe bool
	// Drive is set once the driver has said the minute they are leaving; see
	// internal/drives. The window above is then that minute, and Scheduled is
	// the window that was published.
	DriveSetAt                         *time.Time
	ScheduledEarliest, ScheduledLatest *time.Time
	DetourMin                          int
	// PickupAt is when the car reaches the caller's own pickup on a drive,
	// worked out from the minute it leaves and how far along the road the
	// pickup is. Nil for the driver and when no minute has been given.
	PickupAt *time.Time
	// DriveEndedAt is the driver's «Τέλος»; OffersUntil is how long the drive
	// takes new passengers.
	DriveEndedAt, OffersUntil *time.Time
	// Destination is the end of the driver's road, [lon, lat], for the
	// driver only: it is what their maps app is pointed at after a pickup.
	Destination *[2]float64
	// Stops is every pickup and drop-off in the order the road reaches them.
	Stops []TripStop
}

// TripStop is one pickup or drop-off on the trip, placed along the road.
//
// The driver sees all of it. A passenger sees their own stops in full and only
// the kind of everybody else's, which is enough to know "one stop before you"
// and not enough to know where somebody else lives (QA 2026-10-17 U F3).
type TripStop struct {
	Kind string // pickup or dropoff
	Mine bool
	// The rest is nil on another passenger's stop seen by a passenger.
	PassengerID  *uuid.UUID
	Name         *string
	Label        *string
	Point        *[2]float64
	At           *time.Time
	Seats        *int
	Luggage      *string
	LuggageCount *int
}

// CallWindow is how far either side of the trip a phone number is handed to
// the other party: from two hours before the earliest departure to two hours
// after the car should have arrived.
const CallWindow = 2 * time.Hour

var pickupFrom, pickupFromF = match.DriveFrom("o", "$3::timestamptz")

// ErrNotParticipant means the caller has no place on this trip. It is reported
// as a 404 rather than a 403 so trip ids cannot be probed.
var ErrNotParticipant = errors.New("store: not a participant of this trip")

// GetTrip returns the trip card, provided the caller is on the trip.
//
// The driver's plate is included: SPEC.md withholds it until a booking exists,
// and reaching this function already required one.
func GetTrip(ctx context.Context, pool *pgxpool.Pool, occurrenceID, userID uuid.UUID, now time.Time) (TripDetail, error) {
	role, err := ParticipantRole(ctx, pool, occurrenceID, userID)
	if err != nil {
		return TripDetail{}, err
	}

	var d TripDetail
	d.Role = role
	d.OccurrenceID = occurrenceID
	var destination []float64
	var driverPhone string
	var inCallWindow, confirmedSeat bool
	var callUntil time.Time

	err = pool.QueryRow(ctx, `
		SELECT o.depart_earliest, o.depart_latest, o.status, o.route_km, o.route_min, o.seats_free, o.ends_at_port, o.seats_total,
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
		       dp.car_model, dp.car_color, dp.car_plate, u.bio,
		       coalesce((SELECT array_agg(p.tag) FROM user_preferences p WHERE p.user_id = u.id), '{}'),
		       CASE WHEN t.driver_id = $2
		            THEN EXISTS (SELECT 1 FROM bookings b WHERE b.occurrence_id = o.id
		                         AND b.status IN ('confirmed','completed') AND b.driver_confirmed)
		            ELSE coalesce((SELECT b.passenger_confirmed FROM bookings b
		                           WHERE b.occurrence_id = o.id AND b.passenger_id = $2
		                             AND b.status IN ('confirmed','completed')), false)
		       END,
		       -- The passenger's view of the driver: their own completed booking,
		       -- and whether they have rated on it.
		       coalesce((SELECT b.status = 'completed' FROM bookings b
		                 WHERE b.occurrence_id = o.id AND b.passenger_id = $2
		                   AND b.status IN ('confirmed','completed')), false),
		       EXISTS (SELECT 1 FROM bookings b JOIN ratings r ON r.booking_id = b.id
		               WHERE b.occurrence_id = o.id AND b.passenger_id = $2 AND r.rater_id = $2),
		       o.drive_set_at, o.scheduled_earliest, o.scheduled_latest, COALESCE(o.drive_detour_min, o.detour_min),
		       -- From the car's last trusted fix once it has left, the way the
		       -- matcher timed the pickup; from the pinned minute before. A car
		       -- that has left with no fix gives no time rather than a stale one.
		       CASE WHEN o.drive_set_at IS NOT NULL THEN (
		           SELECT `+pickupFrom+` + (ST_LineLocatePoint(o.route_geom, ST_ClosestPoint(o.route_geom, b.pickup_point))
		                                      - `+pickupFromF+`) * o.route_min * interval '1 minute'
		             FROM bookings b
		            WHERE b.occurrence_id = o.id AND b.passenger_id = $2 AND b.status = 'confirmed')
		       END,
		       o.drive_ended_at, o.offers_until,
		       CASE WHEN t.driver_id = $2 THEN ARRAY[ST_X(t.destination), ST_Y(t.destination)] END,
		       (SELECT b.seats FROM bookings b
		        WHERE b.occurrence_id = o.id AND b.passenger_id = $2
		          AND b.status IN ('confirmed','completed')),
		       EXISTS (SELECT 1 FROM route_schedules rs
		               WHERE rs.template_id = o.template_id AND rs.kind = 'recurring'),
		       u.phone,
		       $3::timestamptz BETWEEN o.depart_earliest - $4::interval
		                           AND o.depart_latest + o.route_min * interval '1 minute' + $4::interval,
		       o.depart_latest + o.route_min * interval '1 minute' + $4::interval,
		       EXISTS (SELECT 1 FROM bookings b
		               WHERE b.occurrence_id = o.id AND b.passenger_id = $2 AND b.status = 'confirmed')
		FROM trip_occurrences o
		JOIN route_templates t  ON t.id = o.template_id
		JOIN driver_profiles dp ON dp.user_id = t.driver_id
		JOIN users u            ON u.id = t.driver_id
		`+match.DriveCar("o", "$3")+`
		WHERE o.id = $1`, occurrenceID, userID, now, CallWindow).Scan(
		&d.DepartEarliest, &d.DepartLatest, &d.Status, &d.RouteKm, &d.RouteMin, &d.SeatsFree, &d.EndsAtPort, &d.SeatsTotal,
		&d.OriginLabel, &d.DestLabel, &d.TemplateID, &d.Weekday, &d.BookingID, &d.RoutePolyline,
		&d.Driver.UserID, &d.Driver.Name, &d.Driver.PhotoVersion, &d.Driver.FacebookHandle, &d.Driver.InstagramHandle, &d.Driver.Rating,
		&d.Driver.CarModel, &d.Driver.CarColor, &d.Driver.CarPlate,
		&d.Driver.Bio, &d.Driver.Preferences, &d.ConfirmedByMe, &d.Driver.CanRate, &d.Driver.Rated,
		&d.DriveSetAt, &d.ScheduledEarliest, &d.ScheduledLatest, &d.DetourMin, &d.PickupAt,
		&d.DriveEndedAt, &d.OffersUntil, &destination, &d.Seats, &d.Repeats, &driverPhone, &inCallWindow, &callUntil, &confirmedSeat)
	if errors.Is(err, pgx.ErrNoRows) {
		return TripDetail{}, ErrNotParticipant
	}
	if err != nil {
		return TripDetail{}, fmt.Errorf("store: loading trip: %w", err)
	}
	d.Driver.CanRate = d.Driver.CanRate && !d.Driver.Rated
	// A passenger whose booking is still confirmed, inside the window; nobody
	// else is handed it. A completed booking is a trip that is over.
	if role == RolePassenger && confirmedSeat && inCallWindow {
		d.Driver.Phone = &driverPhone
		d.Driver.CallUntil = &callUntil
	}
	if len(destination) == 2 {
		d.Destination = &[2]float64{destination[0], destination[1]}
	}

	rows, err := pool.Query(ctx, `
		SELECT u.id, u.name,
		       (SELECT encode(substring(sha256 from 1 for 6), 'hex') FROM profile_photos WHERE user_id = u.id),
		       u.facebook_handle, u.instagram_handle,
		       (SELECT round(avg(stars)::numeric, 2) FROM ratings WHERE ratee_id = u.id),
		       b.price_eur, b.seats, b.pricing_basis, u.bio, b.status = 'completed',
		       coalesce((SELECT array_agg(p.tag) FROM user_preferences p WHERE p.user_id = u.id), '{}'),
		       EXISTS (SELECT 1 FROM ratings r WHERE r.booking_id = b.id AND r.rater_id = $2),
		       b.arrived_at, ST_X(b.pickup_point), ST_Y(b.pickup_point), b.id,
		       ST_X(b.dropoff_point), ST_Y(b.dropoff_point), u.phone
		FROM bookings b JOIN users u ON u.id = b.passenger_id
		WHERE b.occurrence_id = $1 AND b.status IN ('confirmed','completed')
		ORDER BY b.created_at`, occurrenceID, userID)
	if err != nil {
		return TripDetail{}, fmt.Errorf("store: loading passengers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p TripParticipant
		var raw []byte
		var completed bool
		var lon, lat, offLon, offLat float64
		var bookingID uuid.UUID
		var phone string
		if err := rows.Scan(&p.UserID, &p.Name, &p.PhotoVersion, &p.FacebookHandle, &p.InstagramHandle, &p.Rating,
			&p.PriceEUR, &p.Seats, &raw, &p.Bio, &completed, &p.Preferences, &p.Rated,
			&p.ArrivedAt, &lon, &lat, &bookingID, &offLon, &offLat, &phone); err != nil {
			return TripDetail{}, fmt.Errorf("store: scanning passenger: %w", err)
		}
		if role == RoleDriver || p.UserID == userID {
			p.Pickup = &[2]float64{lon, lat}
			p.Dropoff = &[2]float64{offLon, offLat}
		}
		if role == RoleDriver {
			p.BookingID = &bookingID
			if inCallWindow && !completed {
				p.Phone = &phone
				p.CallUntil = &callUntil
			}
		}
		// Only the driver rates passengers, and only once each booking is
		// complete, which is what RateTrip itself insists on.
		p.Rated = role == RoleDriver && p.Rated
		p.CanRate = role == RoleDriver && completed && !p.Rated
		if p.UserID == userID && p.PriceEUR != nil {
			d.PriceEUR = *p.PriceEUR
			// Only the caller's own. A receipt says what somebody paid and what
			// their driver's car drinks; it is not a thing to hand the rest of
			// the car.
			if len(raw) > 0 {
				var basis domain.Basis
				if err := json.Unmarshal(raw, &basis); err == nil {
					d.Basis = &basis
				}
				// A receipt that will not parse is a receipt that cannot be
				// shown, not a trip that cannot be loaded.
			}
		}
		// What somebody else pays, and how many seats they took, is between
		// them and the driver (QA 2026-10-17 A 6).
		if role != RoleDriver && p.UserID != userID {
			p.PriceEUR, p.Seats = nil, nil
		}
		d.Passengers = append(d.Passengers, p)
	}
	if err := rows.Err(); err != nil {
		return TripDetail{}, err
	}
	rows.Close()
	d.Stops, err = tripStops(ctx, pool, occurrenceID, userID, role)
	return d, err
}

// tripStops lists the trip's pickups and drop-offs along the road, each timed
// at the departure plus its share of the route's minutes. A drop-off and a
// pickup at the same place are in that order: somebody gets out first.
func tripStops(ctx context.Context, pool *pgxpool.Pool, occurrenceID, userID uuid.UUID, role TripRole) ([]TripStop, error) {
	rows, err := pool.Query(ctx, `
		WITH s AS (
			SELECT 'pickup' AS kind, b.passenger_id, u.name, rr.origin_label AS label, b.pickup_point AS pt,
			       b.seats, rr.luggage::text AS luggage, b.luggage_count
			  FROM bookings b JOIN users u ON u.id = b.passenger_id
			  LEFT JOIN ride_requests rr ON rr.id = b.request_id
			 WHERE b.occurrence_id = $1 AND b.status IN ('confirmed','completed')
			UNION ALL
			SELECT 'dropoff', b.passenger_id, u.name, rr.destination_label, b.dropoff_point,
			       b.seats, rr.luggage::text, b.luggage_count
			  FROM bookings b JOIN users u ON u.id = b.passenger_id
			  LEFT JOIN ride_requests rr ON rr.id = b.request_id
			 WHERE b.occurrence_id = $1 AND b.status IN ('confirmed','completed')
		)
		-- The scheduled time at each stop: the departure plus its share of the
		-- route. Not the live estimate a drive's car fix gives; pickup_at is that.
		SELECT s.kind, s.passenger_id, s.name, s.label, ST_X(s.pt), ST_Y(s.pt),
		       o.depart_earliest + ST_LineLocatePoint(o.route_geom, ST_ClosestPoint(o.route_geom, s.pt))
		                           * o.route_min * interval '1 minute',
		       s.seats, s.luggage, s.luggage_count
		  FROM s JOIN trip_occurrences o ON o.id = $1
		 ORDER BY ST_LineLocatePoint(o.route_geom, ST_ClosestPoint(o.route_geom, s.pt)),
		          s.kind = 'pickup', s.passenger_id`, occurrenceID)
	if err != nil {
		return nil, fmt.Errorf("store: loading stops: %w", err)
	}
	defer rows.Close()
	out := []TripStop{}
	for rows.Next() {
		var st TripStop
		var pid uuid.UUID
		var name string
		var label, luggage *string
		var lon, lat float64
		var at time.Time
		var seats, count int
		if err := rows.Scan(&st.Kind, &pid, &name, &label, &lon, &lat, &at, &seats, &luggage, &count); err != nil {
			return nil, fmt.Errorf("store: scanning a stop: %w", err)
		}
		st.Mine = pid == userID
		if role == RoleDriver || st.Mine {
			st.PassengerID, st.Name, st.Label = &pid, &name, label
			st.Point, st.At = &[2]float64{lon, lat}, &at
			st.Seats, st.Luggage, st.LuggageCount = &seats, luggage, &count
		}
		out = append(out, st)
	}
	return out, rows.Err()
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

	// A ride can only be confirmed once the car cannot still be waiting.
	//
	// Measured from the far end of the departure window rather than the near
	// one. A trip that may leave any time between 07:00 and 07:30 has not
	// happened at 07:01, and confirming there was the app agreeing that a ride
	// took place while the driver was still at the kerb — which is the whole of
	// what a confirmation is worth, since it is what unlocks a rating.
	//
	// It still only proves the clock passed. Two people who agree to can confirm
	// a journey neither made, and every rating in the app rests on that; closing
	// it needs evidence a ride happened rather than a better timestamp. This is
	// the least misleading line available without that evidence, and RISKS.md
	// says so out loud.
	var departed bool
	err = tx.QueryRow(ctx,
		`SELECT depart_latest <= $2 FROM trip_occurrences WHERE id = $1 FOR UPDATE`,
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

	// Where this passenger actually got in and out, as stored on their own
	// booking rather than as the route's endpoints. It is what makes a past
	// ride repeatable from the search screen: a label alone names a town, and
	// the app has no geocoder to turn one back into a point.
	OriginLon float64
	OriginLat float64
	DestLon   float64
	DestLat   float64
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
		       t.origin_label, t.destination_label, b.price_eur, o.seats_free, b.seats,
		       b.id, u.name,
		       ST_X(b.pickup_point), ST_Y(b.pickup_point),
		       ST_X(b.dropoff_point), ST_Y(b.dropoff_point)
		FROM bookings b
		JOIN trip_occurrences o ON o.id = b.occurrence_id
		JOIN route_templates t  ON t.id = o.template_id
		JOIN users u            ON u.id = t.driver_id
		WHERE b.passenger_id = $1
		  AND b.status IN ('confirmed','completed')
		  AND `+tripEndsAt("o")+` < $2
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
			&p.RouteKm, &p.OriginLabel, &p.DestLabel, &p.PriceEUR, &p.SeatsFree, &p.Seats,
			&p.BookingID, &p.DriverName,
			&p.OriginLon, &p.OriginLat, &p.DestLon, &p.DestLat); err != nil {
			return nil, fmt.Errorf("store: scanning a past trip: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

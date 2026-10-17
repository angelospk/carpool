package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Offer is a seat a driver has offered a passenger who is waiting on a weekend
// plan. It carries what the passenger decides on: who, in what car, when, for
// how much and how far out of the way.
type Offer struct {
	ID           uuid.UUID
	RequestID    uuid.UUID
	OccurrenceID uuid.UUID
	DriverID     uuid.UUID
	DriverName   string
	// PhotoVersion is nil when the driver has no photo.
	PhotoVersion *string
	// Rating is nil while nobody has rated the driver.
	Rating           *float64
	RatingCount      int
	CarMake          string
	CarModel         string
	CarColour        string
	DepartEarliest   time.Time
	PriceEUR         float64
	Seats            int
	DetourMin        float64
	DetourKm         float64
	OriginLabel      string
	DestinationLabel string
	ExpiresAt        time.Time
}

// OffersTo lists the driver offers still waiting on this passenger, soonest to
// lapse first. A lapsed one is not listed while its row still says pending,
// and neither is one from a driver who is no longer active or who is blocked
// either way: accepting would be refused.
func OffersTo(ctx context.Context, pool *pgxpool.Pool, passengerID uuid.UUID, now time.Time) ([]Offer, error) {
	rows, err := pool.Query(ctx, `
		SELECT a.id, r.id, a.occurrence_id, d.id, d.name,
		       (SELECT encode(substring(sha256 from 1 for 6), 'hex') FROM profile_photos WHERE user_id = d.id),
		       (SELECT round(avg(stars)::numeric, 2)::float8 FROM ratings WHERE ratee_id = d.id),
		       (SELECT count(*) FROM ratings WHERE ratee_id = d.id),
		       v.make, v.model, coalesce(v.colour, ''),
		       o.depart_earliest, a.suggested_price_eur::float8, r.seats,
		       a.detour_min::float8, a.detour_km::float8,
		       coalesce(r.origin_label, ''), coalesce(r.destination_label, ''), a.expires_at
		FROM asks a
		JOIN ride_requests r    ON r.id = a.request_id
		JOIN trip_occurrences o ON o.id = a.occurrence_id
		JOIN route_templates t  ON t.id = o.template_id
		JOIN users d            ON d.id = t.driver_id
		JOIN vehicles v         ON v.id = o.vehicle_id
		WHERE r.passenger_id = $1 AND a.offered_by = 'driver'
		  AND a.status = 'pending' AND a.expires_at > $2
		  AND d.status = 'active'
		  AND NOT EXISTS (SELECT 1 FROM blocks b
		                  WHERE (b.blocker_id = t.driver_id AND b.blocked_id = r.passenger_id)
		                     OR (b.blocker_id = r.passenger_id AND b.blocked_id = t.driver_id))
		ORDER BY a.expires_at, a.id
		LIMIT 20`, passengerID, now)
	if err != nil {
		return nil, fmt.Errorf("store: listing offers: %w", err)
	}
	defer rows.Close()

	out := []Offer{}
	for rows.Next() {
		var o Offer
		if err := rows.Scan(&o.ID, &o.RequestID, &o.OccurrenceID, &o.DriverID, &o.DriverName,
			&o.PhotoVersion, &o.Rating, &o.RatingCount, &o.CarMake, &o.CarModel, &o.CarColour,
			&o.DepartEarliest, &o.PriceEUR, &o.Seats, &o.DetourMin, &o.DetourKm,
			&o.OriginLabel, &o.DestinationLabel, &o.ExpiresAt); err != nil {
			return nil, fmt.Errorf("store: scanning offer: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

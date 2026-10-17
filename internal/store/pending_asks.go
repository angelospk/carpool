package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PendingAsk is a question waiting on a driver: somebody wants a seat on one
// of their departures. It carries what the driver decides on — who, which
// journey, how far out of the way, for how much — and nothing that locates the
// passenger more closely than the labels they typed.
type PendingAsk struct {
	ID            uuid.UUID
	OccurrenceID  uuid.UUID
	PassengerID   uuid.UUID
	PassengerName string
	// PhotoVersion is nil when the passenger has no photo.
	PhotoVersion *string
	// Rating is nil while nobody has rated the passenger.
	Rating           *float64
	RatingCount      int
	Luggage          string
	LuggageCount     int
	WindowStart      time.Time
	WindowEnd        time.Time
	OriginLabel      string
	DestinationLabel string
	DetourMin        float64
	DetourKm         float64
	PriceEUR         float64
	Seats            int
	DepartEarliest   time.Time
	ExpiresAt        time.Time
}

// PendingAsks lists the asks still waiting on this driver, soonest to lapse
// first. The push is not the only way to learn of one: a missed notification
// used to mean a missed passenger.
//
// An ask whose expiry has passed is not listed even while its row still says
// pending, because the sweeper that moves it on runs on its own schedule and
// accepting it would only be refused.
func PendingAsks(ctx context.Context, pool *pgxpool.Pool, driverID uuid.UUID, now time.Time) ([]PendingAsk, error) {
	return queryAsks(ctx, pool, `t.driver_id = $1 AND a.status = 'pending' AND a.expires_at > $2
		ORDER BY a.expires_at, a.id LIMIT 20`, driverID, now)
}

// AskFor returns one ask, provided it was sent to this driver and is still
// open. Anything else — somebody else's, answered, lapsed, or no such id — is
// ErrNotParticipant, so ask ids cannot be probed.
func AskFor(ctx context.Context, pool *pgxpool.Pool, askID, driverID uuid.UUID, now time.Time) (PendingAsk, error) {
	list, err := queryAsks(ctx, pool, `t.driver_id = $1 AND a.status = 'pending' AND a.expires_at > $2
		AND a.id = $3`, driverID, now, askID)
	if err != nil {
		return PendingAsk{}, err
	}
	if len(list) == 0 {
		return PendingAsk{}, ErrNotParticipant
	}
	return list[0], nil
}

func queryAsks(ctx context.Context, pool *pgxpool.Pool, where string, args ...any) ([]PendingAsk, error) {
	rows, err := pool.Query(ctx, `
		SELECT a.id, a.occurrence_id, u.id, u.name,
		       (SELECT encode(substring(sha256 from 1 for 6), 'hex') FROM profile_photos WHERE user_id = u.id),
		       (SELECT round(avg(stars)::numeric, 2)::float8 FROM ratings WHERE ratee_id = u.id),
		       (SELECT count(*) FROM ratings WHERE ratee_id = u.id),
		       r.luggage::text, r.luggage_count::int, r.window_start, r.window_end,
		       coalesce(r.origin_label, ''), coalesce(r.destination_label, ''),
		       a.detour_min::float8, a.detour_km::float8, a.suggested_price_eur::float8,
		       r.seats, o.depart_earliest, a.expires_at
		FROM asks a
		JOIN ride_requests r    ON r.id = a.request_id
		JOIN users u            ON u.id = r.passenger_id
		JOIN trip_occurrences o ON o.id = a.occurrence_id
		JOIN route_templates t  ON t.id = o.template_id
		-- An ask that accepting would refuse is not one to show: a passenger
		-- no longer active, or a block made since in either direction, which
		-- accepting checks and the ask row does not remember.
		WHERE u.status = 'active'
		  AND NOT EXISTS (SELECT 1 FROM blocks b
		                  WHERE (b.blocker_id = t.driver_id AND b.blocked_id = r.passenger_id)
		                     OR (b.blocker_id = r.passenger_id AND b.blocked_id = t.driver_id))
		  AND `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("store: listing asks: %w", err)
	}
	defer rows.Close()

	out := []PendingAsk{}
	for rows.Next() {
		var a PendingAsk
		if err := rows.Scan(&a.ID, &a.OccurrenceID, &a.PassengerID, &a.PassengerName,
			&a.PhotoVersion, &a.Rating, &a.RatingCount, &a.Luggage, &a.LuggageCount, &a.WindowStart, &a.WindowEnd,
			&a.OriginLabel, &a.DestinationLabel,
			&a.DetourMin, &a.DetourKm, &a.PriceEUR, &a.Seats, &a.DepartEarliest, &a.ExpiresAt); err != nil {
			return nil, fmt.Errorf("store: scanning ask: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

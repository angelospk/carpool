// Package live shares where people are during a trip they are on together.
//
// It exists for the two minutes that decide whether somebody gets in a car
// with a stranger again: standing at a corner at 07:25 not knowing whether the
// driver is coming, and a driver circling a village square looking for
// somebody they have never seen.
//
// What it is careful about is who sees what, and for how long:
//
//   - The driver sees each passenger with a confirmed seat, and each passenger
//     sees the driver. Passengers do not see each other: two strangers sharing
//     a back seat have no reason to know where the other one lives.
//   - Only around the trip: from half an hour before the earliest departure to
//     an hour after the trip could have arrived. Outside it nothing is written
//     and nothing is read, whatever the phone sends.
//   - One row per person per trip, overwritten. Nothing here is a history. The
//     row goes when the person stops sharing, and the janitor removes what a
//     dead phone leaves behind.
package live

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
)

// ErrNotOnTrip means the caller is not the driver or a passenger holding a
// confirmed seat, or the trip does not exist. One error for both, so trip ids
// cannot be probed.
var ErrNotOnTrip = errors.New("live: not on this trip")

// ErrOutsideWindow means the trip is not close enough in time to share for,
// or it was called off.
var ErrOutsideWindow = errors.New("live: not while this trip is not happening")

// ErrBadPosition means the coordinates are not a place on Earth.
var ErrBadPosition = errors.New("live: that is not a position")

const (
	// Before is how early sharing opens: long enough to walk to the stop and
	// see the car coming.
	Before = 30 * time.Minute
	// After is how long it stays open past when the trip could have arrived.
	After = time.Hour
	// maxRouteTime caps the trip's own length in the window, so that a corrupt
	// route_min cannot keep sharing open for days.
	maxRouteTime = 6 * time.Hour
	// Fresh is how old a position may be and still be shown. Past it the
	// phone has stopped sending, and a dot on a map that is ten minutes old
	// reads as "the car is here" when it is not.
	Fresh = 10 * time.Minute
	// minInterval is the fastest one person's position is written. A phone
	// sending faster is not refused, just not stored more often than this.
	minInterval = 5 * time.Second
)

// Position is one person's last known place on a trip.
type Position struct {
	UserID    uuid.UUID
	Name      string
	Lat, Lon  float64
	UpdatedAt time.Time
}

// Service stores and reads live positions.
type Service struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewService returns a service reading time from c.
func NewService(pool *pgxpool.Pool, c clock.Clock) *Service {
	return &Service{pool: pool, clock: c}
}

// trip is what deciding access needs to know about an occurrence and a caller.
type trip struct {
	driverID  uuid.UUID
	isDriver  bool
	opens     time.Time
	closes    time.Time
	cancelled bool
}

// load decides whether userID is on the trip, and when sharing is open for it.
func (s *Service) load(ctx context.Context, occurrenceID, userID uuid.UUID) (trip, error) {
	var (
		t                trip
		earliest, latest time.Time
		routeMin         int
		status           string
		hasSeat          bool
	)
	err := s.pool.QueryRow(ctx, `
		SELECT t.driver_id, o.depart_earliest, o.depart_latest, o.route_min, o.status,
		       EXISTS (SELECT 1 FROM bookings b
		               WHERE b.occurrence_id = o.id AND b.passenger_id = $2 AND b.status = 'confirmed')
		FROM trip_occurrences o JOIN route_templates t ON t.id = o.template_id
		WHERE o.id = $1`, occurrenceID, userID).
		Scan(&t.driverID, &earliest, &latest, &routeMin, &status, &hasSeat)
	if errors.Is(err, pgx.ErrNoRows) {
		return trip{}, ErrNotOnTrip
	}
	if err != nil {
		return trip{}, fmt.Errorf("live: loading the trip: %w", err)
	}
	t.isDriver = t.driverID == userID
	if !t.isDriver && !hasSeat {
		return trip{}, ErrNotOnTrip
	}
	length := time.Duration(routeMin) * time.Minute
	if length < 0 || length > maxRouteTime {
		length = maxRouteTime
	}
	t.opens = earliest.Add(-Before)
	t.closes = latest.Add(length + After)
	t.cancelled = status == "cancelled"
	return t, nil
}

func (t trip) open(now time.Time) bool {
	return !t.cancelled && !now.Before(t.opens) && now.Before(t.closes)
}

// Window reports when sharing is open for a trip the caller is on, so the
// app can offer it at the right moment rather than guessing from the clock.
func (s *Service) Window(ctx context.Context, occurrenceID, userID uuid.UUID) (opens, closes time.Time, err error) {
	t, err := s.load(ctx, occurrenceID, userID)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return t.opens, t.closes, nil
}

// Put records where the caller is now.
func (s *Service) Put(ctx context.Context, occurrenceID, userID uuid.UUID, lat, lon float64) error {
	if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) ||
		lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return ErrBadPosition
	}
	now := s.clock.Now()
	t, err := s.load(ctx, occurrenceID, userID)
	if err != nil {
		return err
	}
	if !t.open(now) {
		return ErrOutsideWindow
	}
	// The WHERE on the update is the rate limit: a write inside minInterval of
	// the last one leaves the row as it was.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO trip_live_positions (occurrence_id, user_id, lat, lon, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (occurrence_id, user_id) DO UPDATE
		SET lat = EXCLUDED.lat, lon = EXCLUDED.lon, updated_at = EXCLUDED.updated_at
		WHERE trip_live_positions.updated_at <= EXCLUDED.updated_at - $6::interval`,
		occurrenceID, userID, lat, lon, now, fmt.Sprintf("%d seconds", int(minInterval.Seconds()))); err != nil {
		return fmt.Errorf("live: storing the position: %w", err)
	}
	return nil
}

// Stop forgets where the caller is on this trip. Allowed whatever the window
// and whatever became of the booking: stopping must always work.
func (s *Service) Stop(ctx context.Context, occurrenceID, userID uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `
		DELETE FROM trip_live_positions WHERE occurrence_id = $1 AND user_id = $2`,
		occurrenceID, userID); err != nil {
		return fmt.Errorf("live: forgetting the position: %w", err)
	}
	return nil
}

// Visible returns the fresh positions the caller may see on this trip: every
// confirmed passenger for the driver, the driver for a passenger.
func (s *Service) Visible(ctx context.Context, occurrenceID, userID uuid.UUID) ([]Position, error) {
	now := s.clock.Now()
	t, err := s.load(ctx, occurrenceID, userID)
	if err != nil {
		return nil, err
	}
	if !t.open(now) {
		return []Position{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT p.user_id, u.name, p.lat, p.lon, p.updated_at
		FROM trip_live_positions p JOIN users u ON u.id = p.user_id
		WHERE p.occurrence_id = $1
		  AND p.updated_at > $2
		  AND p.user_id <> $3
		  AND CASE WHEN $4 THEN EXISTS (
		             SELECT 1 FROM bookings b
		             WHERE b.occurrence_id = p.occurrence_id AND b.passenger_id = p.user_id
		               AND b.status = 'confirmed')
		           ELSE p.user_id = $5 END
		ORDER BY u.name, p.user_id`,
		occurrenceID, now.Add(-Fresh), userID, t.isDriver, t.driverID)
	if err != nil {
		return nil, fmt.Errorf("live: reading positions: %w", err)
	}
	defer rows.Close()
	out := []Position{}
	for rows.Next() {
		var p Position
		if err := rows.Scan(&p.UserID, &p.Name, &p.Lat, &p.Lon, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("live: reading positions: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

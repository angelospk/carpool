package live

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"carpool/internal/geo"
)

const (
	// etaEvery is the most often the road is asked about one passenger.
	// A phone polling every few seconds gets the same answer in between.
	etaEvery = time.Minute
	// etaFixMaxAge is how old the driver's fix may be and still say how far
	// away they are. Older than this the car may be anywhere.
	etaFixMaxAge = 2 * time.Minute
	// etaKeep is how long a cached answer is kept after it was computed.
	etaKeep = 15 * time.Minute
)

// Router computes a driving route through waypoints. *geo.OSRM satisfies it.
type Router interface {
	Route(ctx context.Context, waypoints []geo.Point) (geo.Route, error)
}

// ETA is how long the driver needs to reach a passenger's pickup point.
type ETA struct {
	Minutes int
	// At is when it was computed.
	At time.Time
}

type etaKey struct{ occurrence, passenger uuid.UUID }

type etaEntry struct {
	eta        ETA
	ok         bool // false: the router failed, remembered so it is not asked again at once
	computedAt time.Time
	fixAt      time.Time
}

type etaCache struct {
	mu      sync.Mutex
	entries map[etaKey]etaEntry
}

// WithRouter makes DriverETA available. Without a router it answers nothing.
func (s *Service) WithRouter(r Router) *Service {
	s.router = r
	return s
}

// DriverETA says how many minutes the driver is from the pickup point of
// userID, by road, from the driver's latest fix.
//
// It answers nil, with no error, when it cannot say: the caller is the driver
// or has no confirmed seat, there is no router, the driver has no fix from
// the last two minutes, or the road could not be asked. The road is asked at
// most once a minute for one passenger on one trip, and again only when the
// driver has reported since.
func (s *Service) DriverETA(ctx context.Context, occurrenceID, userID uuid.UUID) (*ETA, error) {
	if s.router == nil {
		return nil, nil
	}
	now := s.clock.Now()
	t, err := s.load(ctx, occurrenceID, userID)
	if err != nil {
		return nil, err
	}
	if t.isDriver || !t.open(now) {
		return nil, nil
	}

	var (
		fix        geo.Point
		fixAt      time.Time
		pickup     geo.Point
		haveFix    = true
		havePickup = true
	)
	err = s.pool.QueryRow(ctx, `
		SELECT p.lon, p.lat, COALESCE(p.observed_at, p.updated_at)
		FROM trip_live_positions p
		WHERE p.occurrence_id = $1 AND p.user_id = $2`,
		occurrenceID, t.driverID).Scan(&fix.Lon, &fix.Lat, &fixAt)
	if errors.Is(err, pgx.ErrNoRows) {
		haveFix = false
	} else if err != nil {
		return nil, fmt.Errorf("live: reading the driver's fix: %w", err)
	}
	if !haveFix || fixAt.Before(now.Add(-etaFixMaxAge)) {
		return nil, nil
	}

	key := etaKey{occurrenceID, userID}
	cached, seen := s.eta.get(key)
	if seen && (now.Sub(cached.computedAt) < etaEvery || !fixAt.After(cached.fixAt)) {
		return cached.answer(), nil
	}

	err = s.pool.QueryRow(ctx, `
		SELECT ST_X(pickup_point), ST_Y(pickup_point) FROM bookings
		WHERE occurrence_id = $1 AND passenger_id = $2 AND status = 'confirmed'`,
		occurrenceID, userID).Scan(&pickup.Lon, &pickup.Lat)
	if errors.Is(err, pgx.ErrNoRows) {
		havePickup = false
	} else if err != nil {
		return nil, fmt.Errorf("live: reading the pickup point: %w", err)
	}
	if !havePickup {
		return nil, nil
	}

	entry := etaEntry{computedAt: now, fixAt: fixAt}
	route, err := s.router.Route(ctx, []geo.Point{fix, pickup})
	if err == nil {
		entry.ok = true
		entry.eta = ETA{Minutes: int((route.Duration + 30*time.Second) / time.Minute), At: now}
	}
	s.eta.put(key, entry, now)
	return entry.answer(), nil
}

func (e etaEntry) answer() *ETA {
	if !e.ok {
		return nil
	}
	out := e.eta
	return &out
}

func (c *etaCache) get(k etaKey) (etaEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	return e, ok
}

func (c *etaCache) put(k etaKey, e etaEntry, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[etaKey]etaEntry{}
	}
	for old, v := range c.entries {
		if now.Sub(v.computedAt) > etaKeep {
			delete(c.entries, old)
		}
	}
	c.entries[k] = e
}

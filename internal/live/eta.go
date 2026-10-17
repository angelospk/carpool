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
	// pending is set while one caller is asking the road; it closes when that
	// caller has an answer. Everybody else for the same key waits on it
	// instead of asking again.
	pending chan struct{}
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
	cached, wait := s.eta.claim(key, now, fixAt)
	if wait != nil {
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		answer, _ := s.eta.get(key)
		return answer.answer(), nil
	}
	if cached != nil {
		return cached.answer(), nil
	}

	// This caller owns the ask. The claim is settled on every way out, or
	// the others would wait for ever.
	settled := false
	defer func() {
		if !settled {
			s.eta.abandon(key)
		}
	}()

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
	} else if ctx.Err() != nil {
		// The caller went away; that says nothing about the road.
		return nil, ctx.Err()
	}
	s.eta.finish(key, entry)
	settled = true
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

// claim decides, in one step under the lock, what a caller does for k: use the
// kept answer (the first result), wait for the ask already under way (the
// second), or ask the road itself, in which case both are nil and an in-flight
// entry now stands for the caller until finish or abandon.
func (c *etaCache) claim(k etaKey, now, fixAt time.Time) (kept *etaEntry, wait chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[etaKey]etaEntry{}
	}
	if e, ok := c.entries[k]; ok {
		if e.pending != nil {
			return nil, e.pending
		}
		if now.Sub(e.computedAt) < etaEvery || !fixAt.After(e.fixAt) {
			return &e, nil
		}
	}
	for old, v := range c.entries {
		if v.pending == nil && now.Sub(v.computedAt) > etaKeep {
			delete(c.entries, old)
		}
	}
	c.entries[k] = etaEntry{computedAt: now, fixAt: fixAt, pending: make(chan struct{})}
	return nil, nil
}

// finish stores the owner's answer and wakes the callers waiting for it.
func (c *etaCache) finish(k etaKey, e etaEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p := c.entries[k].pending; p != nil {
		close(p)
	}
	c.entries[k] = e
}

// abandon drops an in-flight entry that has no answer, and wakes the callers
// waiting for it; they find nothing and answer nothing.
func (c *etaCache) abandon(k etaKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p := c.entries[k].pending; p != nil {
		close(p)
		delete(c.entries, k)
	}
}

package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// perIP is a fixed-window request counter per client address, for the few
// routes anybody can call without an account.
//
// A route link's token cannot be guessed, so this is not what protects it. It
// is what stops somebody who has one — or who has none and is trying — from
// turning a public page into a way to keep the database busy. In memory,
// because the service runs as one process; a restart forgets the counts,
// which costs nothing but a fresh minute.
type perIP struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	seen   map[string]*ipWindow
}

type ipWindow struct {
	start time.Time
	n     int
}

func newPerIP(limit int, window time.Duration, now func() time.Time) *perIP {
	return &perIP{limit: limit, window: window, now: now, seen: map[string]*ipWindow{}}
}

func (p *perIP) allow(addr string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	// Dropping every expired window whenever the map grows keeps it bounded by
	// the addresses seen in one window, without a goroutine to sweep it.
	if len(p.seen) > 10_000 {
		for k, w := range p.seen {
			if now.Sub(w.start) >= p.window {
				delete(p.seen, k)
			}
		}
	}
	w, ok := p.seen[addr]
	if !ok || now.Sub(w.start) >= p.window {
		p.seen[addr] = &ipWindow{start: now, n: 1}
		return true
	}
	w.n++
	return w.n <= p.limit
}

// middleware refuses a client over the limit with 429. The address is the one
// the connection came from; behind Caddy that is Caddy, so X-Real-IP — which
// Caddy sets and a client cannot, because Caddy overwrites it — is preferred.
func (p *perIP) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addr := r.Header.Get("X-Real-IP")
		if addr == "" {
			addr, _, _ = net.SplitHostPort(r.RemoteAddr)
		}
		if !p.allow(addr) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "too_many", "slow down")
			return
		}
		next.ServeHTTP(w, r)
	})
}

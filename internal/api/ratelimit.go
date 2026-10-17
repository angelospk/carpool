package api

import (
	"net"
	"net/http"
	"strconv"
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
//
// Trusting the header is safe in production: deploy/vps/Caddyfile sets
// X-Real-IP from Cf-Connecting-Ip with header_up, which replaces any value the
// client sent, and the API listens on 127.0.0.1 only (deploy/vps/compose.yml),
// so nothing reaches it without passing Caddy. A development listener on a LAN
// (scripts/dev-lan.sh) has no proxy in front and trusts whatever the client
// sends, so a client there can pick its own address.
func (p *perIP) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addr := r.Header.Get("X-Real-IP")
		if addr == "" {
			addr, _, _ = net.SplitHostPort(r.RemoteAddr)
		}
		if !p.allow(addr) {
			p.refuse(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// perUser is the same counter keyed on the signed-in caller rather than the
// address, for things only an account can do. It has to sit inside requireAuth.
func (p *perIP) perUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := callerID(r.Context())
		if ok && !p.allow("user:"+id.String()) {
			p.refuse(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// refuse answers 429. The code is the one the per-number sign-in limit already
// uses, so the app has one thing to recognise; Retry-After is the window, not a
// guess, because the sign-in limit is counted by the hour.
func (p *perIP) refuse(w http.ResponseWriter) {
	w.Header().Set("Retry-After", strconv.Itoa(int(p.window.Seconds())))
	writeError(w, http.StatusTooManyRequests, "rate_limited", "slow down")
}

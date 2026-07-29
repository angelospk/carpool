// Package clock provides the time source used across the service.
//
// Everything that reads the wall clock takes a Clock rather than calling
// time.Now directly, so OTP expiry, ask TTLs, outbox backoff and recurrence
// materialization are all testable without sleeping.
package clock

import (
	"sync"
	"time"
)

// Clock reports the current time.
type Clock interface {
	Now() time.Time
}

// Real is the production clock.
type Real struct{}

// Now returns the current wall-clock time.
func (Real) Now() time.Time { return time.Now() }

// Fake is a manually advanced clock for tests. It is safe for concurrent use so
// that concurrency tests can share one clock across goroutines.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

// NewFake returns a Fake positioned at seed.
func NewFake(seed time.Time) *Fake { return &Fake{now: seed} }

// Now returns the fake's current instant.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance moves the fake forward by d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// Set moves the fake to an absolute instant.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
}

package live_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"carpool/internal/geo"
	"carpool/internal/live"
)

// countingRouter answers every route as eight minutes and counts the asks.
type countingRouter struct {
	mu    sync.Mutex
	calls int
	err   error
	last  []geo.Point
}

func (r *countingRouter) Route(_ context.Context, wp []geo.Point) (geo.Route, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.last = wp
	if r.err != nil {
		return geo.Route{}, r.err
	}
	return geo.Route{Duration: 8 * time.Minute}, nil
}

func (r *countingRouter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func etaWorld(t *testing.T) (*world, *countingRouter) {
	t.Helper()
	w := newWorld(t)
	r := &countingRouter{}
	w.svc.WithRouter(r)
	return w, r
}

func TestThePassengerGetsTheDriversETAToTheirPickup(t *testing.T) {
	w, r := etaWorld(t)
	ctx := context.Background()
	w.everybodyShares(t)

	eta, err := w.svc.DriverETA(ctx, w.trip, w.rider)
	if err != nil || eta == nil {
		t.Fatalf("DriverETA = %v, %v; want an answer", eta, err)
	}
	if eta.Minutes != 8 || !eta.At.Equal(w.clock.Now()) {
		t.Fatalf("eta = %+v, want 8 minutes computed now", eta)
	}
	if len(r.last) != 2 || r.last[0] != (geo.Point{Lon: 25.9, Lat: 40.85}) || r.last[1] != (geo.Point{Lon: 25.82, Lat: 40.85}) {
		t.Fatalf("routed %v, want driver fix to the rider's pickup", r.last)
	}
}

func TestTheRoadIsAskedOnceAMinute(t *testing.T) {
	w, r := etaWorld(t)
	ctx := context.Background()
	w.everybodyShares(t)

	for i := 0; i < 3; i++ {
		if eta, _ := w.svc.DriverETA(ctx, w.trip, w.rider); eta == nil {
			t.Fatalf("request %d: no answer", i)
		}
		w.clock.Advance(10 * time.Second)
	}
	if r.count() != 1 {
		t.Fatalf("router called %d times within a minute, want 1", r.count())
	}
}

func TestAfterAMinuteAndANewFixTheRoadIsAskedAgain(t *testing.T) {
	w, r := etaWorld(t)
	ctx := context.Background()
	w.everybodyShares(t)
	_, _ = w.svc.DriverETA(ctx, w.trip, w.rider)

	w.clock.Advance(61 * time.Second)
	if err := w.svc.Put(ctx, w.trip, w.driver, 40.85, 25.86); err != nil {
		t.Fatal(err)
	}
	eta, _ := w.svc.DriverETA(ctx, w.trip, w.rider)
	if eta == nil || !eta.At.Equal(w.clock.Now()) {
		t.Fatalf("eta = %+v, want one computed now", eta)
	}
	if r.count() != 2 {
		t.Fatalf("router called %d times, want 2", r.count())
	}
}

// A minute on without the driver reporting is not a reason to ask again.
func TestAMinuteOldAnswerIsKeptUntilTheDriverReports(t *testing.T) {
	w, r := etaWorld(t)
	ctx := context.Background()
	w.everybodyShares(t)
	_, _ = w.svc.DriverETA(ctx, w.trip, w.rider)
	w.clock.Advance(90 * time.Second)
	if eta, _ := w.svc.DriverETA(ctx, w.trip, w.rider); eta == nil {
		t.Fatal("the answer for the same fix vanished")
	}
	if r.count() != 1 {
		t.Fatalf("router called %d times, want 1", r.count())
	}
}

func TestAStaleDriverFixGivesNoETA(t *testing.T) {
	w, r := etaWorld(t)
	ctx := context.Background()
	w.everybodyShares(t)
	w.clock.Advance(2*time.Minute + time.Second)
	eta, err := w.svc.DriverETA(ctx, w.trip, w.rider)
	if err != nil || eta != nil {
		t.Fatalf("DriverETA = %v, %v; want absent", eta, err)
	}
	if r.count() != 0 {
		t.Fatalf("router called %d times for a stale fix", r.count())
	}
}

func TestNoDriverFixGivesNoETA(t *testing.T) {
	w, _ := etaWorld(t)
	if eta, err := w.svc.DriverETA(context.Background(), w.trip, w.rider); err != nil || eta != nil {
		t.Fatalf("DriverETA = %v, %v; want absent", eta, err)
	}
}

func TestTheDriverAndOutsidersGetNoETA(t *testing.T) {
	w, r := etaWorld(t)
	ctx := context.Background()
	w.everybodyShares(t)
	if eta, err := w.svc.DriverETA(ctx, w.trip, w.driver); err != nil || eta != nil {
		t.Fatalf("the driver got %v, %v", eta, err)
	}
	if _, err := w.svc.DriverETA(ctx, w.trip, w.nobody); !errors.Is(err, live.ErrNotOnTrip) {
		t.Fatalf("an outsider: %v, want ErrNotOnTrip", err)
	}
	if r.count() != 0 {
		t.Fatalf("router called %d times", r.count())
	}
}

// Each passenger has their own pickup, so their own answer and their own ask.
func TestEachPassengerHasTheirOwnETA(t *testing.T) {
	w, r := etaWorld(t)
	ctx := context.Background()
	w.everybodyShares(t)
	_, _ = w.svc.DriverETA(ctx, w.trip, w.rider)
	_, _ = w.svc.DriverETA(ctx, w.trip, w.other)
	if r.count() != 2 {
		t.Fatalf("router called %d times, want 2", r.count())
	}
}

func TestARouterErrorGivesNoETAAndIsNotRetriedAtOnce(t *testing.T) {
	w, r := etaWorld(t)
	ctx := context.Background()
	r.err = errors.New("osrm down")
	w.everybodyShares(t)
	for i := 0; i < 2; i++ {
		if eta, err := w.svc.DriverETA(ctx, w.trip, w.rider); err != nil || eta != nil {
			t.Fatalf("DriverETA = %v, %v; want absent and no error", eta, err)
		}
	}
	if r.count() != 1 {
		t.Fatalf("router called %d times, want 1", r.count())
	}
}

func TestWithoutARouterThereIsNoETA(t *testing.T) {
	w := newWorld(t)
	w.everybodyShares(t)
	if eta, err := w.svc.DriverETA(context.Background(), w.trip, w.rider); err != nil || eta != nil {
		t.Fatalf("DriverETA = %v, %v; want absent", eta, err)
	}
}

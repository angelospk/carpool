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

// slowRouter holds every ask until released, so simultaneous callers really
// are simultaneous.
type slowRouter struct {
	countingRouter
	release chan struct{}
}

func (r *slowRouter) Route(ctx context.Context, wp []geo.Point) (geo.Route, error) {
	<-r.release
	return r.countingRouter.Route(ctx, wp)
}

// askTogether has n callers ask for the rider's ETA at once and returns what
// they got once the router is let go.
func askTogether(t *testing.T, w *world, r *slowRouter, n int) []*live.ETA {
	t.Helper()
	got := make([]*live.ETA, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			eta, err := w.svc.DriverETA(context.Background(), w.trip, w.rider)
			if err != nil {
				t.Errorf("caller %d: %v", i, err)
			}
			got[i] = eta
		}(i)
	}
	time.Sleep(200 * time.Millisecond)
	close(r.release)
	wg.Wait()
	return got
}

func TestSimultaneousPollsAskTheRoadOnce(t *testing.T) {
	w := newWorld(t)
	r := &slowRouter{release: make(chan struct{})}
	w.svc.WithRouter(r)
	w.everybodyShares(t)

	got := askTogether(t, w, r, 20)
	if r.count() != 1 {
		t.Fatalf("router called %d times for 20 simultaneous polls, want 1", r.count())
	}
	for i, eta := range got {
		if eta == nil || eta.Minutes != 8 || !eta.At.Equal(got[0].At) {
			t.Fatalf("caller %d got %+v, want the same 8 minutes as the rest", i, eta)
		}
	}
}

func TestSimultaneousPollsAfterAMinuteAndANewFixAskTheRoadOnceMore(t *testing.T) {
	w, first := etaWorld(t)
	ctx := context.Background()
	w.everybodyShares(t)
	_, _ = w.svc.DriverETA(ctx, w.trip, w.rider)

	w.clock.Advance(61 * time.Second)
	if err := w.svc.Put(ctx, w.trip, w.driver, 40.85, 25.86); err != nil {
		t.Fatal(err)
	}
	r := &slowRouter{release: make(chan struct{})}
	w.svc.WithRouter(r)
	got := askTogether(t, w, r, 20)
	if first.count() != 1 || r.count() != 1 {
		t.Fatalf("router called %d then %d times, want 1 then 1", first.count(), r.count())
	}
	for i, eta := range got {
		if eta == nil || !eta.At.Equal(w.clock.Now()) {
			t.Fatalf("caller %d got %+v, want one computed now", i, eta)
		}
	}
}

// ctxRouter gives up when its caller's context does, as an HTTP router does.
// It says when it has been entered, and when it gave up.
type ctxRouter struct {
	countingRouter
	entered chan struct{}
	aborted chan struct{}
	release chan struct{}
}

func (r *ctxRouter) Route(ctx context.Context, wp []geo.Point) (geo.Route, error) {
	r.entered <- struct{}{}
	select {
	case <-r.release:
		return r.countingRouter.Route(ctx, wp)
	case <-ctx.Done():
		r.mu.Lock()
		r.calls++
		r.mu.Unlock()
		close(r.aborted)
		return geo.Route{}, ctx.Err()
	}
}

// Codex review: a poll that goes away mid-ask must not free the key for an
// immediate second ask; the road is still asked at most once a minute.
func TestACancelledPollKeepsTheMinute(t *testing.T) {
	w := newWorld(t)
	r := &ctxRouter{
		entered: make(chan struct{}, 2),
		aborted: make(chan struct{}),
		release: make(chan struct{}),
	}
	w.svc.WithRouter(r)
	w.everybodyShares(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = w.svc.DriverETA(ctx, w.trip, w.rider)
	}()
	<-r.entered
	cancel()
	// The road is asked on a context of its own: the poll going away must not
	// reach it. Tied to the poll, the router gives up at once.
	select {
	case <-r.aborted:
		t.Fatal("the routing call was cancelled with the poll")
	case <-time.After(200 * time.Millisecond):
	}
	close(r.release)
	<-done

	if _, err := w.svc.DriverETA(context.Background(), w.trip, w.rider); err != nil {
		t.Fatal(err)
	}
	if r.count() != 1 {
		t.Fatalf("router called %d times within the minute, want 1", r.count())
	}
}

package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().String()
}

func TestServerServesUntilShutdown(t *testing.T) {
	addr := freePort(t)
	srv := NewServer(addr, NewRouter(Deps{Ready: func(context.Context) error { return nil }}))

	var wg sync.WaitGroup
	wg.Add(1)
	var serveErr error
	go func() {
		defer wg.Done()
		serveErr = srv.Run(context.Background())
	}()

	waitForHealthy(t, addr)

	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	wg.Wait()

	// A deliberate shutdown is not an error condition.
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		t.Fatalf("Run returned %v, want nil or http.ErrServerClosed", serveErr)
	}
}

func TestServerStopsWhenItsContextIsCancelled(t *testing.T) {
	addr := freePort(t)
	srv := NewServer(addr, NewRouter(Deps{Ready: func(context.Context) error { return nil }}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	waitForHealthy(t, addr)
	cancel() // stands in for SIGTERM

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Run returned %v, want nil or http.ErrServerClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s of its context being cancelled")
	}
}

func TestServerFinishesInFlightRequestsBeforeStopping(t *testing.T) {
	addr := freePort(t)

	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("finished"))
	})
	srv := NewServer(addr, mux)

	go func() { _ = srv.Run(context.Background()) }()
	waitForListening(t, addr)

	// Start a request, then shut down while it is still in flight.
	type result struct {
		body string
		err  error
	}
	got := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/slow")
		if err != nil {
			got <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		got <- result{body: string(b), err: err}
	}()

	waitForListening(t, addr)
	shutdownDone := make(chan error, 1)
	go func() {
		time.Sleep(50 * time.Millisecond) // let the request reach the handler
		shutdownDone <- srv.Shutdown(context.Background())
	}()

	time.Sleep(150 * time.Millisecond)
	close(release) // let the handler finish

	select {
	case r := <-got:
		if r.err != nil {
			t.Fatalf("the in-flight request failed instead of completing: %v", r.err)
		}
		if r.body != "finished" {
			t.Fatalf("body = %q, want %q", r.body, "finished")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the in-flight request never completed")
	}

	if err := <-shutdownDone; err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func waitForListening(t *testing.T, addr string) {
	t.Helper()
	for range 100 {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server never began listening on %s", addr)
}

func waitForHealthy(t *testing.T, addr string) {
	t.Helper()
	for range 100 {
		resp, err := http.Get(fmt.Sprintf("http://%s/v1/livez", addr))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server never became healthy on %s", addr)
}

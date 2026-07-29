package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// shutdownGrace bounds how long in-flight requests get to finish once shutdown
// begins. Anything still running after this is cut off so a wedged handler
// cannot block a deploy indefinitely.
const shutdownGrace = 20 * time.Second

// Server wraps http.Server with the lifecycle the rest of the process expects:
// Run blocks until the server stops, and stopping is always graceful.
type Server struct {
	http *http.Server
}

// NewServer returns a Server bound to addr serving h.
func NewServer(addr string, h http.Handler) *Server {
	return &Server{
		http: &http.Server{
			Addr:    addr,
			Handler: h,
			// Bound the slow-client attack surface. WriteTimeout is generous
			// because matching can wait on OSRM.
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
		},
	}
}

// Run serves until the server is shut down or ctx is cancelled. Cancelling ctx
// triggers a graceful shutdown, which is how SIGTERM is handled.
func (s *Server) Run(ctx context.Context) error {
	serveErr := make(chan error, 1)
	go func() {
		err := s.http.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		if err := s.Shutdown(context.WithoutCancel(ctx)); err != nil {
			return err
		}
		return <-serveErr
	}
}

// Shutdown stops accepting new connections and waits for in-flight requests,
// up to shutdownGrace.
func (s *Server) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, shutdownGrace)
	defer cancel()

	if err := s.http.Shutdown(ctx); err != nil {
		return fmt.Errorf("api: graceful shutdown: %w", err)
	}
	return nil
}

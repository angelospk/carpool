package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// wsWriteTimeout bounds how long a single frame write may take before the
// connection is considered stuck.
const wsWriteTimeout = 10 * time.Second

// handleTripSocket streams a trip's live events to a foregrounded app.
//
// The socket carries chat only; it is never the source of truth. A client that
// reconnects refetches over REST, so a dropped frame costs latency and nothing
// else.
func (d Deps) handleTripSocket(w http.ResponseWriter, r *http.Request) {
	_, occurrenceID, _, ok := d.tripParams(w, r)
	if !ok {
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The app is a native client with no browser Origin, and the token was
		// already validated by the auth middleware.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return // Accept has already written a response
	}
	defer func() { _ = conn.CloseNow() }()

	sub, unsubscribe := d.Hub.Subscribe(occurrenceID)
	defer unsubscribe()

	ctx := r.Context()

	// Reading serves two purposes: it surfaces client disconnects promptly, and
	// it lets the library answer pings. Inbound frames are ignored — messages
	// are posted over REST so they are persisted before they are published.
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case frame, open := <-sub.Messages():
			if !open {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
			err := conn.Write(writeCtx, websocket.MessageText, frame)
			cancel()
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					_ = conn.Close(websocket.StatusInternalError, "write failed")
				}
				return
			}
		}
	}
}

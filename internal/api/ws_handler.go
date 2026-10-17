package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"carpool/internal/store"
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
	userID, occurrenceID, _, ok := d.tripParams(w, r)
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
			// Who is on the trip is checked again for every frame, not only at
			// the upgrade: a passenger who cancelled their seat, was dropped
			// with the departure, or was suspended must stop reading the car's
			// chat (QA 2026-10-17 A 2). Chat is a few messages a trip, so a
			// query per frame costs nothing worth saving.
			if !d.stillOnTrip(ctx, occurrenceID, userID) {
				_ = conn.Close(websocket.StatusPolicyViolation, "no longer on this trip")
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

// stillOnTrip reports whether userID is an active account taking part in the
// occurrence. A failed lookup counts as no: the socket is never the source of
// truth, and the app refetches over REST.
func (d Deps) stillOnTrip(ctx context.Context, occurrenceID, userID uuid.UUID) bool {
	if _, err := store.ParticipantRole(ctx, d.Pool, occurrenceID, userID); err != nil {
		return false
	}
	var active bool
	if err := d.Pool.QueryRow(ctx,
		`SELECT status = 'active' FROM users WHERE id = $1`, userID).Scan(&active); err != nil {
		return false
	}
	return active
}

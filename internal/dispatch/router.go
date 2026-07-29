// Package dispatch routes outbox messages to the effects that carry them out.
//
// It sits above both outbox and push so neither has to know about the other:
// push classifies its own failures using outbox's sentinel, and outbox stays a
// generic queue.
package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/outbox"
	"carpool/internal/push"
)

// Router dispatches an outbox message to the right effect based on its kind.
type Router struct {
	pool *pgxpool.Pool
	push push.Sender
	// AskNext is called for 'ask_next' messages to approach the next driver.
	// It is a function rather than an interface to avoid an import cycle with
	// the asks package.
	askNext func(ctx context.Context, requestID uuid.UUID) error
}

// NewRouter returns a handler covering every message kind the system enqueues.
func NewRouter(pool *pgxpool.Pool, sender push.Sender, askNext func(context.Context, uuid.UUID) error) *Router {
	return &Router{pool: pool, push: sender, askNext: askNext}
}

// Handle performs the effect a message describes.
func (r *Router) Handle(ctx context.Context, m outbox.Message) error {
	switch m.Kind {
	case "push":
		return r.handlePush(ctx, m)
	case "ask_next":
		return r.handleAskNext(ctx, m)
	default:
		// An unknown kind will never become known by retrying.
		return fmt.Errorf("%w: unknown outbox kind %q", outbox.ErrPermanent, m.Kind)
	}
}

// pushPayload covers the fields any push-kind message may carry. Which ids are
// present depends on the event.
type pushPayload struct {
	AskID       *uuid.UUID `json:"ask_id"`
	DriverID    *uuid.UUID `json:"driver_id"`
	PassengerID *uuid.UUID `json:"passenger_id"`
	BookingID   *uuid.UUID `json:"booking_id"`
	DetourMin   float64    `json:"detour_min"`
}

func (r *Router) handlePush(ctx context.Context, m outbox.Message) error {
	var p pushPayload
	if err := json.Unmarshal(m.Payload, &p); err != nil {
		return fmt.Errorf("%w: undecodable push payload: %v", outbox.ErrPermanent, err)
	}

	recipient, title, body := p.notification()
	if recipient == nil {
		return fmt.Errorf("%w: push payload names no recipient", outbox.ErrPermanent)
	}

	tokens, err := r.tokensFor(ctx, *recipient)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		// Nothing to deliver to. Retrying will not conjure a device; the user
		// reconciles over REST when they next open the app.
		slog.Info("no live push tokens for recipient", "user", *recipient, "key", m.IdempotencyKey)
		return nil
	}

	// The idempotency key travels with the notification so the app can
	// deduplicate a push it has already acted on.
	data := map[string]any{"idempotency_key": m.IdempotencyKey}
	if p.AskID != nil {
		data["ask_id"] = p.AskID.String()
	}
	if p.BookingID != nil {
		data["booking_id"] = p.BookingID.String()
	}

	var failures error
	for _, token := range tokens {
		err := r.push.Send(ctx, push.Notification{
			Token: token, Title: title, Body: body, Data: data,
		})
		if errors.Is(err, push.ErrDeviceNotRegistered) {
			// Prune the dead token, but do not fail the message: the user's
			// other devices may have received it.
			if _, dbErr := r.pool.Exec(ctx,
				`UPDATE push_tokens SET disabled_at = now() WHERE token = $1`, token); dbErr != nil {
				slog.Error("disabling dead push token", "error", dbErr)
			}
			continue
		}
		if err != nil {
			failures = errors.Join(failures, err)
		}
	}
	return failures
}

// notification picks the recipient and copy for an event.
func (p pushPayload) notification() (recipient *uuid.UUID, title, body string) {
	switch {
	case p.BookingID != nil && p.PassengerID != nil:
		return p.PassengerID, "Η διαδρομή σου επιβεβαιώθηκε",
			"Ο οδηγός δέχτηκε το αίτημά σου."
	case p.AskID != nil && p.DriverID != nil:
		return p.DriverID, "Νέο αίτημα διαδρομής",
			fmt.Sprintf("Ένας επιβάτης ζητά μεταφορά, +%.0f λεπτά.", p.DetourMin)
	default:
		return nil, "", ""
	}
}

func (r *Router) tokensFor(ctx context.Context, userID uuid.UUID) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT token FROM push_tokens WHERE user_id = $1 AND disabled_at IS NULL`, userID)
	if err != nil {
		return nil, fmt.Errorf("outbox: loading push tokens: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("outbox: scanning push token: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Router) handleAskNext(ctx context.Context, m outbox.Message) error {
	var p struct {
		RequestID uuid.UUID `json:"request_id"`
	}
	if err := json.Unmarshal(m.Payload, &p); err != nil {
		return fmt.Errorf("%w: undecodable ask_next payload: %v", outbox.ErrPermanent, err)
	}
	if r.askNext == nil {
		return errors.New("outbox: no ask_next handler configured")
	}
	return r.askNext(ctx, p.RequestID)
}

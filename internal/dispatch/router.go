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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/match"
	"carpool/internal/notices"
	"carpool/internal/outbox"
	"carpool/internal/push"
)

// Router dispatches an outbox message to the right effect based on its kind.
type Router struct {
	pool  *pgxpool.Pool
	clock clock.Clock
	push  push.Sender
	// AskNext is called for 'ask_next' messages to approach the next driver.
	// It is a function rather than an interface to avoid an import cycle with
	// the asks package.
	askNext func(ctx context.Context, requestID uuid.UUID) error
}

// NewRouter returns a handler covering every message kind the system enqueues.
//
// A nil askNext is a wiring fault, and it is refused here rather than at
// delivery time: a router that discovers it at delivery time can only either
// dead-letter the ask — destroying it, when a restart would have fixed the
// worker — or retry a message no retry can help.
func NewRouter(pool *pgxpool.Pool, c clock.Clock, sender push.Sender, askNext func(context.Context, uuid.UUID) error) *Router {
	if askNext == nil {
		panic("dispatch: NewRouter needs an ask_next handler")
	}
	return &Router{pool: pool, clock: c, push: sender, askNext: askNext}
}

// Handle performs the effect a message describes.
func (r *Router) Handle(ctx context.Context, m outbox.Message) error {
	switch m.Kind {
	case "push":
		return r.handlePush(ctx, m)
	case "ask_next":
		return r.handleAskNext(ctx, m)
	case "wake_waiting":
		return r.handleWake(ctx, m)
	case "reopen_request":
		return r.handleReopen(ctx, m)
	default:
		// An unknown kind will never become known by retrying.
		return fmt.Errorf("%w: unknown outbox kind %q", outbox.ErrPermanent, m.Kind)
	}
}

// pushPayload covers the fields a push-kind message may carry.
//
// Two shapes arrive here. One says what happened — see internal/notices — and
// is the only shape anything new may write. The other says nothing and leaves
// the recipient to be inferred from which ids are present; it is what the two
// oldest producers write, and it is read here so that a row queued by the
// version running before a deploy is still deliverable by the version after it.
type pushPayload struct {
	Event         notices.Event `json:"event"`
	RecipientID   uuid.UUID     `json:"recipient_id"`
	ArrangementID *uuid.UUID    `json:"arrangement_id"`

	AskID       *uuid.UUID `json:"ask_id"`
	DriverID    *uuid.UUID `json:"driver_id"`
	PassengerID *uuid.UUID `json:"passenger_id"`
	BookingID   *uuid.UUID `json:"booking_id"`
	DetourMin   float64    `json:"detour_min"`

	OccurrenceID *uuid.UUID `json:"occurrence_id"`
	Count        int        `json:"count"`
}

func (r *Router) handlePush(ctx context.Context, m outbox.Message) error {
	var p pushPayload
	if err := json.Unmarshal(m.Payload, &p); err != nil {
		return fmt.Errorf("%w: undecodable push payload: %v", outbox.ErrPermanent, err)
	}

	recipient, title, body, err := p.notification()
	if err != nil {
		// Permanent, and it names what it could not say. The alternative —
		// logging it and reporting success — is precisely how this went unseen:
		// a producer and a dispatcher that disagree then look exactly like a
		// quiet week. The dead-lettered row keeps its payload, so what was lost
		// can be read afterwards and sent by hand.
		return fmt.Errorf("%w: %v", outbox.ErrPermanent, err)
	}

	tokens, err := r.tokensFor(ctx, recipient)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		// Nothing to deliver to. Retrying will not conjure a device; the user
		// reconciles over REST when they next open the app.
		slog.Info("no live push tokens for recipient", "user", recipient, "key", m.IdempotencyKey)
		return nil
	}

	// The idempotency key travels with the notification so the app can
	// deduplicate a push it has already acted on, and the id of whatever it is
	// about so the app can open that rather than the front page.
	data := map[string]any{"idempotency_key": m.IdempotencyKey}
	if p.AskID != nil {
		data["ask_id"] = p.AskID.String()
	}
	if p.BookingID != nil {
		data["booking_id"] = p.BookingID.String()
	}
	if p.ArrangementID != nil {
		data["arrangement_id"] = p.ArrangementID.String()
	}
	if p.OccurrenceID != nil {
		data["occurrence_id"] = p.OccurrenceID.String()
	}

	// One message fans out to every device the user has, and the devices fail
	// independently. The two kinds of failure are kept apart because joining
	// them would let one device's permanent rejection carry ErrPermanent into
	// the aggregate, and the dispatcher would dead-letter the message — losing
	// the notification for a device that had merely timed out.
	var transient, permanent error
	for _, token := range tokens {
		err := r.push.Send(ctx, push.Notification{
			Token: token, Title: title, Body: body, Data: data,
		})
		switch {
		case err == nil:
			// The provider took it, so this device is real and somebody is
			// carrying it. Recorded because the retention rule ages tokens by
			// last use, and a use nobody writes down is a device that looks
			// abandoned — the rule would then quietly stop notifications for
			// people who use the app daily.
			//
			// GREATEST so the write is monotonic: two deliveries finishing out
			// of order must not move the timestamp backwards.
			//
			// A failure here is logged and not returned. The notification was
			// delivered; turning a bookkeeping error into a retry would send it
			// twice, and the cost of losing one such write is that a live token
			// looks a few hours staler than it is against a two-year cutoff.
			if _, dbErr := r.pool.Exec(ctx,
				`UPDATE push_tokens
				    SET last_used_at = GREATEST($2, COALESCE(last_used_at, $2))
				  WHERE token = $1`,
				token, r.clock.Now()); dbErr != nil {
				slog.Warn("recording push token use", "error", dbErr)
			}
		case errors.Is(err, push.ErrDeviceNotRegistered):
			// Prune the dead token, but do not fail the message: the user's
			// other devices may have received it.
			if _, dbErr := r.pool.Exec(ctx,
				`UPDATE push_tokens SET disabled_at = $2 WHERE token = $1`,
				token, r.clock.Now()); dbErr != nil {
				// Not merely a logging matter: reporting success would mark the
				// message delivered while the dead token stays live, and every
				// later notification for this user would burn on it.
				transient = errors.Join(transient, fmt.Errorf("dispatch: retiring a dead push token: %w", dbErr))
			}
		case errors.Is(err, outbox.ErrPermanent):
			permanent = errors.Join(permanent, err)
		default:
			transient = errors.Join(transient, err)
		}
	}

	// A device that can still be reached outranks one that cannot. Retrying
	// costs the reachable devices a duplicate, which the app drops on the
	// idempotency key; dead-lettering costs them the notification outright.
	if transient != nil {
		if permanent != nil {
			slog.Warn("giving up on a device but retrying the message",
				"key", m.IdempotencyKey, "error", permanent)
		}
		return transient
	}
	return permanent
}

// notification picks the recipient and copy for an event.
// notification works out who is told and what they read.
//
// An event, when there is one, is the whole answer and the payload is never
// second-guessed from the ids around it: a notice that says what happened and
// carries no recipient is a broken notice, not an invitation to deliver it to
// whoever else is named. Guessing is what the two shapes below do, and it is
// kept only for the rows written before there was anything better.
func (p pushPayload) notification() (recipient uuid.UUID, title, body string, err error) {
	if p.Event == "" {
		var to uuid.UUID
		switch {
		case p.BookingID != nil && p.PassengerID != nil:
			to, title, body = *p.PassengerID, "Η διαδρομή σου επιβεβαιώθηκε",
				"Ο οδηγός δέχτηκε το αίτημά σου."
		case p.AskID != nil && p.DriverID != nil:
			to, title, body = *p.DriverID, "Νέο αίτημα διαδρομής",
				fmt.Sprintf("Ένας επιβάτης ζητά μεταφορά, +%.0f λεπτά.", p.DetourMin)
		default:
			return uuid.Nil, "", "", errors.New("push payload names neither an event nor a recipient")
		}
		if to == uuid.Nil {
			return uuid.Nil, "", "", errors.New("push payload names the zero user")
		}
		return to, title, body, nil
	}

	// What the words are is decided before the payload is checked to be
	// complete, so that an event nobody has written words for is reported as
	// exactly that rather than as a missing field.
	title, body, ok := say(p.Event, p.Count)
	if !ok {
		return uuid.Nil, "", "", fmt.Errorf("nothing to say for event %q", p.Event)
	}
	if err := (notices.Payload{
		Event: p.Event, RecipientID: p.RecipientID,
		BookingID: p.BookingID, ArrangementID: p.ArrangementID,
		OccurrenceID: p.OccurrenceID, Count: p.Count,
	}).Validate(); err != nil {
		return uuid.Nil, "", "", err
	}
	return p.RecipientID, title, body, nil
}

// say is the app's whole vocabulary: every event, and the words somebody reads
// on a lock screen because of it.
//
// A cancellation says what is true for whoever is reading it and does not
// account for itself. The same fact reaches a passenger whether the driver
// called the trip off, closed their account or was suspended, and only the
// first of those is the driver's doing — so none of the three is claimed.
func say(event notices.Event, count int) (title, body string, ok bool) {
	switch event {
	case notices.DriveRetimed:
		return "Ο οδηγός είπε πότε φεύγει",
			"Άνοιξε τη διαδρομή για να δεις πότε περνά από το σημείο σου.", true
	case notices.DriveNudge:
		title := "Κάποιος στον δρόμο σου θέλει να πάει τώρα"
		if count > 1 {
			title = fmt.Sprintf("%d άτομα στον δρόμο σου θέλουν να πάνε τώρα", count)
		}
		return title,
			"Έχεις ελεύθερες θέσεις. Πες πότε φεύγεις και τους ρωτάμε.", true
	case notices.BookingCancelledForPassenger:
		// The second sentence is true whichever of the three it was: each of
		// them puts the passenger's request back in the queue if it came from
		// one. It is conditional because a seat from a standing arrangement had
		// no request behind it, and a lock screen cannot tell the two apart.
		return "Η διαδρομή ακυρώθηκε",
			"Η κράτησή σου δεν ισχύει πια. Αν το αίτημά σου ισχύει ακόμα, ψάχνουμε ήδη άλλον οδηγό.", true
	case notices.BookingCancelledForDriver:
		return "Ακυρώθηκε μια κράτηση",
			"Ένας επιβάτης δεν θα έρθει τελικά, και η θέση είναι πάλι ελεύθερη.", true
	case notices.ArrangementRequested:
		return "Νέο αίτημα για σταθερή διαδρομή",
			"Κάποιος ζητά να έρχεται μαζί σου τακτικά.", true
	case notices.ArrangementApproved:
		return "Η σταθερή διαδρομή εγκρίθηκε",
			"Ο οδηγός δέχτηκε να ταξιδεύετε μαζί τακτικά.", true
	case notices.ArrangementDeclined:
		return "Το αίτημα δεν έγινε δεκτό",
			"Ο οδηγός δεν μπορεί να σε παίρνει τακτικά.", true
	case notices.ArrangementCancelled:
		return "Η σταθερή διαδρομή σταμάτησε",
			"Οι επόμενες εβδομάδες ακυρώθηκαν.", true
	default:
		return "", "", false
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
	if p.RequestID == uuid.Nil {
		return fmt.Errorf("%w: ask_next payload names no request", outbox.ErrPermanent)
	}
	return r.askNext(ctx, p.RequestID)
}

// WakeShortlist bounds how many waiting requests one trip wakes at a time.
//
// The rest are not lost: they keep their place in the queue, which is ordered
// by how long each has waited, and the next thing that happens to this trip
// wakes them. A driver declining or letting an offer lapse is such a thing, so
// the queue drains on its own as the conversation moves.
const WakeShortlist = 10

// handleWake asks a trip who has been waiting for it, and puts each of them
// back through the ordinary ask machinery.
//
// A trip that has been cancelled, filled or deleted since the wake was queued
// simply wakes nobody: the query returns no rows and the message is done. That
// is the right answer rather than a failure, because retrying cannot bring a
// cancelled trip back and dead-lettering it would say something went wrong when
// nothing did.
func (r *Router) handleWake(ctx context.Context, m outbox.Message) error {
	var p struct {
		OccurrenceID uuid.UUID `json:"occurrence_id"`
	}
	if err := json.Unmarshal(m.Payload, &p); err != nil {
		return fmt.Errorf("%w: undecodable wake_waiting payload: %v", outbox.ErrPermanent, err)
	}
	if p.OccurrenceID == uuid.Nil {
		return fmt.Errorf("%w: wake_waiting payload names no occurrence", outbox.ErrPermanent)
	}

	waiting, err := match.Waiting(ctx, r.pool, match.WaitingQuery{
		OccurrenceID: p.OccurrenceID,
		Now:          r.clock.Now(),
		Limit:        WakeShortlist,
	})
	if err != nil {
		return err
	}
	if len(waiting) == WakeShortlist {
		// Said out loud rather than left to be inferred. A cap that silently
		// truncates reads afterwards as "everyone was reached".
		slog.Info("waking a shortlist of the requests waiting for a trip",
			"occurrence", p.OccurrenceID, "woken", len(waiting),
			"note", "more may be waiting; they keep their place and the next change to this trip wakes them")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dispatch: begin wake: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, requestID := range waiting {
		payload, err := json.Marshal(map[string]any{"request_id": requestID})
		if err != nil {
			return fmt.Errorf("dispatch: encoding ask_next for %s: %w", requestID, err)
		}
		// Keyed off this wake rather than the clock. The handler is retried
		// whole, so a crash after the seventh of ten children has to produce
		// the same ten keys on the way back through, or the first six arrive
		// twice and the same driver is asked twice about the same trip.
		if err := outbox.Enqueue(ctx, tx, outbox.Message{
			Kind:           "ask_next",
			IdempotencyKey: "ask_next:wake:" + m.IdempotencyKey + ":" + requestID.String(),
			Payload:        payload,
		}, r.clock.Now()); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("dispatch: commit wake: %w", err)
	}
	return nil
}

// handleReopen puts a passenger whose seat was cancelled back in the matching
// queue, and asks the next driver for them.
//
// This is deliberately not done by the transaction that cancelled the seat.
// Suspending an account, closing one, and a driver calling off a departure are
// each one transaction by design — a half-applied suspension is the bug the
// whole cascade exists to prevent — and each of them holds the driver's user
// row, the occurrence and the bookings. Reopening the passengers' requests
// inside that transaction adds a lock on rows belonging to an unbounded set of
// other people, taken after the occurrence and without their user rows, which is
// the one order the rest of the app does not use. Two moderators acting at once
// could then each hold what the other was waiting for.
//
// So the cancelling transaction records what happened and commits, and the
// recovery happens here, where the rows can be taken in the ordinary order:
// the passenger, then their request.
//
// Everything the producer knew is re-read rather than trusted. Between the
// cancellation committing and this running, the passenger may have cancelled
// the request themselves, been suspended, closed their account, been matched to
// somebody else, or run out of window. Each of those makes this message a
// no-op, which is a success and not a failure: there is nothing to retry and
// nothing went wrong.
func (r *Router) handleReopen(ctx context.Context, m outbox.Message) error {
	var p struct {
		BookingID uuid.UUID `json:"booking_id"`
		RequestID uuid.UUID `json:"request_id"`
		Reason    string    `json:"reason"`
	}
	if err := json.Unmarshal(m.Payload, &p); err != nil {
		return fmt.Errorf("%w: undecodable reopen_request payload: %v", outbox.ErrPermanent, err)
	}
	if p.BookingID == uuid.Nil || p.RequestID == uuid.Nil {
		return fmt.Errorf("%w: reopen_request payload names no booking and request", outbox.ErrPermanent)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dispatch: begin reopen: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The booking has to be the one this request most recently had.
	//
	// Being vacant is not the same as having been vacated by this booking, and
	// the difference is reachable: a passenger stranded, rematched and stranded
	// again has two recoveries owed, and the outbox is at-least-once. A replay
	// of the first, arriving after the second driver cancelled, finds exactly
	// what it expects — matched, no live booking — and consumes the reopening
	// the second one was going to do, while carrying the first cancellation's
	// key, which the outbox has already seen and drops. The request is left open
	// with nobody being asked.
	//
	// A request holds at most one confirmed or completed booking at a time, so
	// the newest booking it has is the one that last owned the match, and that
	// is what makes the ordering here an answer about causation rather than
	// about vacancy. The request id is checked against the booking too, rather
	// than trusted alongside it: the payload names both, and nothing else makes
	// them agree.
	var passengerID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT b.passenger_id FROM bookings b
		 WHERE b.id = $1 AND b.request_id = $2
		   AND NOT EXISTS (
		         SELECT 1 FROM bookings later
		          WHERE later.request_id = b.request_id
		            AND (later.created_at, later.id) > (b.created_at, b.id))`,
		p.BookingID, p.RequestID).Scan(&passengerID)
	if errors.Is(err, pgx.ErrNoRows) {
		// The booking is gone, does not belong to that request, or has been
		// superseded by a later match. Nothing to recover, and nothing that
		// retrying fixes.
		return nil
	}
	if err != nil {
		return fmt.Errorf("dispatch: loading the cancelled booking: %w", err)
	}

	// The passenger first, and shared rather than exclusive, which is the order
	// and the strength every other path between two people uses. Suspension and
	// closure take this row exclusively, so either they wait and then see the
	// reopened request, or this waits and then reads the status they wrote.
	var accountStatus string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM users WHERE id = $1 FOR SHARE`, passengerID).Scan(&accountStatus); err != nil {
		return fmt.Errorf("dispatch: locking the passenger: %w", err)
	}
	if accountStatus != "active" {
		return nil
	}

	// One statement decides. A request is reopened only if it is still the one
	// this booking was for: still matched, still inside its window, and with no
	// live booking of its own — which, given that a request may hold at most one
	// confirmed or completed booking at a time, is what "this cancellation is
	// still the most recent thing that happened to it" means.
	tag, err := tx.Exec(ctx, `
		UPDATE ride_requests rr SET status = 'open', updated_at = now()
		 WHERE rr.id = $1
		   AND rr.status = 'matched'
		   AND rr.window_end > $2
		   AND NOT EXISTS (
		         SELECT 1 FROM bookings b
		          WHERE b.request_id = rr.id AND b.status IN ('confirmed','completed'))`,
		p.RequestID, r.clock.Now())
	if err != nil {
		return fmt.Errorf("dispatch: reopening the request: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}

	payload, err := json.Marshal(map[string]any{"request_id": p.RequestID, "reason": p.Reason})
	if err != nil {
		return fmt.Errorf("dispatch: encoding ask_next for %s: %w", p.RequestID, err)
	}
	// Keyed on the seat that was lost, so the same cancellation asks once
	// however many times this message is redelivered, while somebody stranded a
	// second time — rematched, and stranded again by the next driver — is a
	// different booking and so a different key.
	if err := outbox.Enqueue(ctx, tx, outbox.Message{
		Kind:           "ask_next",
		IdempotencyKey: "ask_next:reopen:" + p.BookingID.String(),
		Payload:        payload,
	}, r.clock.Now()); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("dispatch: commit reopen: %w", err)
	}
	return nil
}

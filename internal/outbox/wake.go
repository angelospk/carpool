package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// Wake queues the question "who has been waiting for this trip?".
//
// It goes in the same transaction as whatever made the trip available, and that
// is the point of it being a helper rather than a line of code at each site. A
// wake enqueued after its own transaction commits is a wake that a crash in
// between silently loses, which puts things back exactly as they were before
// any of this existed: a driver publishes, nobody hears, and somebody's request
// expires with a car that had room going the same way.
//
// The reason says which change this was. It is part of the key rather than
// decoration, because a trip becomes available more than once in its life — it
// is published, then somebody cancels, then an offer lapses — and keying on the
// trip alone would make every change after the first a silent no-op.
func Wake(ctx context.Context, t tx, occurrenceID uuid.UUID, reason string, now time.Time) error {
	if occurrenceID == uuid.Nil {
		return fmt.Errorf("outbox: a %s wake names no occurrence", reason)
	}
	payload, err := json.Marshal(map[string]any{"occurrence_id": occurrenceID})
	if err != nil {
		return fmt.Errorf("outbox: encoding a %s wake: %w", reason, err)
	}
	return EnqueueAt(ctx, t, Message{
		Kind: "wake_waiting",
		// The instant makes each change its own event. It is safe here in a way
		// it would not be for a message the dispatcher generates, because this
		// enqueue lives or dies with the transaction around it: a retry rolls
		// the whole thing back rather than leaving a duplicate behind.
		IdempotencyKey: "wake:" + occurrenceID.String() + ":" + reason + ":" +
			strconv.FormatInt(now.UnixNano(), 10),
		Payload: payload,
	}, now)
}

// WakeAll is Wake over the occurrences one change freed seats on.
//
// Cancelling a standing arrangement or closing an account gives seats back on
// every future departure at once, and each of those is a trip that somebody
// waiting on the corridor could now be offered.
func WakeAll(ctx context.Context, t tx, occurrenceIDs []uuid.UUID, reason string, now time.Time) error {
	for _, id := range occurrenceIDs {
		if err := Wake(ctx, t, id, reason, now); err != nil {
			return err
		}
	}
	return nil
}

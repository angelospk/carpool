package asks

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// errNotActive means one of the two people involved is suspended or gone.
//
// Not an exported error, and callers map it onto whatever they already say for
// "this offer is no longer available". A driver holding an ask does not need to
// be told which of the two of them a moderator has acted on, and a passenger
// being matched does not need to hear it either.
var errNotActive = errors.New("asks: one of the participants is not an active account")

// lockParticipants holds both people's rows for the rest of the transaction and
// refuses unless both accounts are active.
//
// FOR SHARE rather than FOR UPDATE: several matches involving the same driver
// may run at once and have no reason to queue behind each other, and what this
// has to conflict with is the one statement that changes a status. Suspension
// and deletion both take the user row FOR UPDATE, so either they wait for this
// transaction and then see the ask or the booking it wrote, or this one waits
// and then reads the new status. What cannot happen is the pair that made this
// a bug: a status read that was true when it was taken, a cascade that swept
// past, and a row written afterwards that nothing will ever revisit.
//
// The ids are sorted so two transactions holding the same pair take them in the
// same order. Every path that creates work between two people goes through
// here, so that ordering is the whole of the convention rather than a rule
// spread across call sites.
func lockParticipants(ctx context.Context, tx pgx.Tx, a, b uuid.UUID) error {
	ids := []uuid.UUID{a, b}
	if ids[1].String() < ids[0].String() {
		ids[0], ids[1] = ids[1], ids[0]
	}

	for _, id := range ids {
		var status string
		if err := tx.QueryRow(ctx,
			`SELECT status FROM users WHERE id = $1 FOR SHARE`, id).Scan(&status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// The row is gone outright rather than marked. Nothing to match.
				return errNotActive
			}
			return fmt.Errorf("asks: locking participant: %w", err)
		}
		if status != "active" {
			return errNotActive
		}
	}
	return nil
}

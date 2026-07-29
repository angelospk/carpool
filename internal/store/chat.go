package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// maxMessageLen mirrors the schema's check constraint.
const maxMessageLen = 2000

// Message is one chat message on a trip thread.
type Message struct {
	ID         uuid.UUID
	SenderID   uuid.UUID
	SenderName string
	Body       string
	CreatedAt  time.Time
}

// PostMessage appends to a trip's thread. Authorization is the caller's job;
// every route reaching here has already checked participation.
func PostMessage(ctx context.Context, pool *pgxpool.Pool, occurrenceID, senderID uuid.UUID, body string) (Message, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Message{}, fmt.Errorf("store: a message cannot be empty")
	}
	if len(body) > maxMessageLen {
		return Message{}, fmt.Errorf("store: a message may be at most %d characters", maxMessageLen)
	}

	var m Message
	err := pool.QueryRow(ctx, `
		INSERT INTO trip_messages (occurrence_id, sender_id, body)
		VALUES ($1, $2, $3)
		RETURNING id, sender_id, body, created_at,
		          (SELECT name FROM users WHERE id = $2)`,
		occurrenceID, senderID, body).Scan(&m.ID, &m.SenderID, &m.Body, &m.CreatedAt, &m.SenderName)
	if err != nil {
		return Message{}, fmt.Errorf("store: posting message: %w", err)
	}
	return m, nil
}

// ListMessages returns a page of a trip's thread, oldest first.
//
// The cursor is (created_at, id), which is total: two messages written in the
// same microsecond still order deterministically, so a page can neither repeat
// nor skip one.
func ListMessages(ctx context.Context, pool *pgxpool.Pool, occurrenceID uuid.UUID,
	afterTime *time.Time, afterID *uuid.UUID, limit int,
) ([]Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	// The cursor is a pair. Half of it is not a smaller cursor, it is a broken
	// one: (created_at, id) > (T, NULL) is unknown for every row at exactly T,
	// so those messages would be skipped forever.
	if (afterTime == nil) != (afterID == nil) {
		return nil, fmt.Errorf("store: a message cursor needs both after_time and after_id, or neither")
	}

	rows, err := pool.Query(ctx, `
		SELECT m.id, m.sender_id, u.name, m.body, m.created_at
		FROM trip_messages m JOIN users u ON u.id = m.sender_id
		WHERE m.occurrence_id = $1
		  AND ($2::timestamptz IS NULL OR (m.created_at, m.id) > ($2, $3))
		ORDER BY m.created_at, m.id
		LIMIT $4`, occurrenceID, afterTime, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: listing messages: %w", err)
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SenderID, &m.SenderName, &m.Body, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scanning message: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// BlockUser records that blocker never wants to be matched with blocked again.
// It is idempotent.
func BlockUser(ctx context.Context, pool *pgxpool.Pool, blockerID, blockedID uuid.UUID) error {
	if blockerID == blockedID {
		return fmt.Errorf("store: a user cannot block themselves")
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, blockerID, blockedID)
	if err != nil {
		return fmt.Errorf("store: blocking user: %w", err)
	}
	return nil
}

// UnblockUser reverses a block.
func UnblockUser(ctx context.Context, pool *pgxpool.Pool, blockerID, blockedID uuid.UUID) error {
	_, err := pool.Exec(ctx,
		`DELETE FROM blocks WHERE blocker_id = $1 AND blocked_id = $2`, blockerID, blockedID)
	if err != nil {
		return fmt.Errorf("store: unblocking user: %w", err)
	}
	return nil
}

// ReportUser files a report. Reports are append-only: they are an audit record,
// so nothing here updates or removes an earlier one.
func ReportUser(ctx context.Context, pool *pgxpool.Pool, reporterID, reportedID uuid.UUID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("store: a report needs a reason")
	}
	if reporterID == reportedID {
		return fmt.Errorf("store: a user cannot report themselves")
	}

	_, err := pool.Exec(ctx,
		`INSERT INTO reports (reporter_id, reported_id, reason) VALUES ($1, $2, $3)`,
		reporterID, reportedID, reason)
	if err != nil {
		return fmt.Errorf("store: filing report: %w", err)
	}
	return nil
}

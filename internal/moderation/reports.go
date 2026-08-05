package moderation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ReportsPerDay is how many reports one person may file in twenty-four hours.
//
// A limit at all because a queue a moderator cannot get through is a queue that
// protects nobody, and one angry person can fill it. Twenty because somebody
// with a genuinely bad week could plausibly need several, and because the point
// is to stop flooding rather than to ration complaints.
const ReportsPerDay = 20

// ErrTooManyReports means the reporter has hit the daily limit.
var ErrTooManyReports = errors.New("moderation: too many reports today")

// ErrAlreadyDecided means somebody else got to this report first. It is not a
// failure — the report has been dealt with — but the caller asked to be the one
// who dealt with it, and was not.
var ErrAlreadyDecided = errors.New("moderation: that report has already been decided")

// ErrNoReport means there is no such report.
var ErrNoReport = errors.New("moderation: no such report")

// Report is one complaint, as a moderator needs to see it.
type Report struct {
	ID         uuid.UUID
	ReporterID uuid.UUID
	ReportedID uuid.UUID
	// ReportedName and ReporterName save the queue from being a list of uuids.
	ReportedName string
	ReporterName string
	Reason       string
	CreatedAt    time.Time
	// PriorReports is how many other times this person has been reported,
	// counting every report against them but this one. One complaint is a
	// disagreement; the fifth is a pattern, and the difference is most of the
	// decision.
	PriorReports int
	// ReportedStatus says whether they are already suspended, so two moderators
	// working the queue do not both act on the same person.
	ReportedStatus string
	// Status is the report's own: open, dismissed or actioned.
	Status string
}

// File records a complaint.
//
// The daily limit is enforced under a lock on the reporter's own row rather
// than by counting and then inserting: two requests arriving together would
// both count nineteen and both insert.
func (s *Service) File(ctx context.Context, reporterID, reportedID uuid.UUID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errors.New("moderation: a report needs a reason")
	}
	if reporterID == reportedID {
		return errors.New("moderation: a user cannot report themselves")
	}
	now := s.clock.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("moderation: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT true FROM users WHERE id = $1 FOR UPDATE`, reporterID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoAccount
		}
		return fmt.Errorf("moderation: locking the reporter: %w", err)
	}

	var today int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM reports
		 WHERE reporter_id = $1 AND created_at > $2`,
		reporterID, now.Add(-24*time.Hour)).Scan(&today); err != nil {
		return fmt.Errorf("moderation: counting today's reports: %w", err)
	}
	if today >= ReportsPerDay {
		return ErrTooManyReports
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO reports (reporter_id, reported_id, reason, created_at)
		VALUES ($1, $2, $3, $4)`, reporterID, reportedID, reason, now); err != nil {
		return fmt.Errorf("moderation: filing the report: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("moderation: commit: %w", err)
	}
	return nil
}

// Open lists the reports nobody has decided yet, oldest first.
//
// Oldest first because the alternative is that a busy week buries last
// Tuesday's complaint permanently.
func (s *Service) Open(ctx context.Context, limit int) ([]Report, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.reporter_id, r.reported_id, reported.name, reporter.name,
		       r.reason, r.created_at, reported.status,
		       (SELECT count(*) FROM reports other
		         WHERE other.reported_id = r.reported_id AND other.id <> r.id)
		FROM reports r
		JOIN users reported ON reported.id = r.reported_id
		JOIN users reporter ON reporter.id = r.reporter_id
		WHERE r.status = 'open'
		ORDER BY r.created_at, r.id
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("moderation: listing reports: %w", err)
	}
	defer rows.Close()

	var out []Report
	for rows.Next() {
		var r Report
		if err := rows.Scan(&r.ID, &r.ReporterID, &r.ReportedID, &r.ReportedName, &r.ReporterName,
			&r.Reason, &r.CreatedAt, &r.ReportedStatus, &r.PriorReports); err != nil {
			return nil, fmt.Errorf("moderation: scanning a report: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Decide closes a report, and suspends the account if the decision was to.
//
// The status change and the suspension are one transaction, and the report is
// claimed by a conditional UPDATE rather than a read followed by a write, so two
// moderators pressing at the same moment produce one decision and one audit
// entry rather than two of each.
func (s *Service) Decide(ctx context.Context, reportID, moderatorID uuid.UUID, suspend bool, resolution string) error {
	resolution = strings.TrimSpace(resolution)
	if resolution == "" {
		return errors.New("moderation: a decision needs a note")
	}
	now := s.clock.Now()

	status := "dismissed"
	if suspend {
		status = "actioned"
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("moderation: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var reportedID uuid.UUID
	err = tx.QueryRow(ctx, `
		UPDATE reports
		   SET status = $2, reviewed_by = $3, reviewed_at = $4, resolution = $5
		 WHERE id = $1 AND status = 'open'
		RETURNING reported_id`, reportID, status, moderatorID, now, resolution).Scan(&reportedID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Either it does not exist or somebody else has already decided it. The
		// two are told apart with a second look, because "already handled" and
		// "no such thing" lead a moderator to do different things next.
		var live bool
		switch err := tx.QueryRow(ctx,
			`SELECT true FROM reports WHERE id = $1`, reportID).Scan(&live); {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrNoReport
		case err != nil:
			// A database that cannot answer is not the same as a report that is
			// not there, and telling a moderator the second when it is the first
			// sends them looking for the wrong thing.
			return fmt.Errorf("moderation: looking up the report: %w", err)
		}
		return ErrAlreadyDecided
	}
	if err != nil {
		return fmt.Errorf("moderation: deciding the report: %w", err)
	}

	if suspend {
		if err := s.suspendTx(ctx, tx, reportedID, moderatorID, resolution, now); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("moderation: commit: %w", err)
	}
	return nil
}

// ByReporter lists what one person has filed, so they can be shown that it went
// somewhere.
func (s *Service) ByReporter(ctx context.Context, reporterID uuid.UUID, limit int) ([]Report, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.reporter_id, r.reported_id, reported.name, reporter.name,
		       r.reason, r.created_at, r.status
		FROM reports r
		JOIN users reported ON reported.id = r.reported_id
		JOIN users reporter ON reporter.id = r.reporter_id
		WHERE r.reporter_id = $1
		ORDER BY r.created_at DESC, r.id
		LIMIT $2`, reporterID, limit)
	if err != nil {
		return nil, fmt.Errorf("moderation: listing my reports: %w", err)
	}
	defer rows.Close()

	var out []Report
	for rows.Next() {
		var r Report
		// The report's status, not the account's. What somebody who filed a
		// report wants to know is whether it was looked at; telling them the
		// other party has been suspended would hand them a fact about a
		// stranger's account.
		if err := rows.Scan(&r.ID, &r.ReporterID, &r.ReportedID, &r.ReportedName, &r.ReporterName,
			&r.Reason, &r.CreatedAt, &r.Status); err != nil {
			return nil, fmt.Errorf("moderation: scanning a report: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

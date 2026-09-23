// Package groups keeps the circles people already belong to: a school, a
// hospital, an office.
//
// It exists because trust between strangers is the thing a carpooling app is
// short of, and "we work in the same building" is the cheapest honest signal
// there is. A group does three things with it. Members see which of the people
// they are matched with share a group with them. A driver can offer a route to
// one group only. And an admin can pull the month's confirmed trips between
// members, which is what lets the organisation pay for them without the app
// ever moving money — see Export.
package groups

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/text/unicode/norm"

	"carpool/internal/clock"
	"carpool/internal/invites"
)

// ErrBadName means the name is empty, too long, or carries characters that
// would make it render as something other than what is stored.
var ErrBadName = errors.New("groups: that name cannot be used")

// ErrCodeNotUsable means the code is not a group's current code. One error for
// every way that can be, for the same reason as invites.ErrCodeNotUsable: an
// answer that distinguishes them helps the person guessing.
var ErrCodeNotUsable = errors.New("groups: that code cannot be used")

// ErrTooManyAttempts means this person has typed too many wrong codes today.
var ErrTooManyAttempts = errors.New("groups: too many wrong codes")

// ErrNotMember means the caller is not a live member of the group. It is also
// what a group that does not exist looks like, so that ids cannot be probed.
var ErrNotMember = errors.New("groups: not a member")

// ErrNotAdmin means the caller is a member but the act needs an admin.
var ErrNotAdmin = errors.New("groups: admins only")

// ErrTooManyGroups means the caller already belongs to as many groups as one
// person may.
var ErrTooManyGroups = errors.New("groups: too many groups")

// FailuresPerDay is how many wrong codes one person may try in twenty-four
// hours. A person mistypes a code two or three times; ten is generous for that
// and useless for a search over 26^8 codes.
const FailuresPerDay = 10

// MaxGroupsPerPerson bounds how many groups one account can be in. A school
// and a job and a club is three; the cap is there so that the "shared groups"
// signal cannot be manufactured by joining everything.
const MaxGroupsPerPerson = 10

const (
	minNameRunes = 2
	maxNameRunes = 60
)

// Group is one group as a member sees it.
type Group struct {
	ID      uuid.UUID
	Name    string
	Role    string
	Members int
	// Code is filled in for admins only. Everybody holding the code can let
	// anybody in, so the fewer screens it is on the better.
	Code string
}

// Service runs groups.
type Service struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewService returns a service reading time from c.
func NewService(pool *pgxpool.Pool, c clock.Clock) *Service {
	return &Service{pool: pool, clock: c}
}

// NormaliseName cleans a group name up, or refuses it.
//
// The same rules as a person's display name — NFC, runs of whitespace become
// one space, no control or invisible formatting characters — because a group
// name is shown beside strangers as a reason to trust them, and one that can
// pretend to be another is worse than none.
func NormaliseName(in string) (string, error) {
	n := strings.Join(strings.Fields(norm.NFC.String(in)), " ")
	for _, r := range n {
		if r == ' ' {
			continue
		}
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) || r == unicode.ReplacementChar {
			return "", ErrBadName
		}
	}
	if l := len([]rune(n)); l < minNameRunes || l > maxNameRunes {
		return "", ErrBadName
	}
	return n, nil
}

// Create makes a group with the caller as its only member and admin.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, name string) (Group, error) {
	clean, err := NormaliseName(name)
	if err != nil {
		return Group{}, err
	}
	now := s.clock.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Group{}, fmt.Errorf("groups: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.roomForOneMore(ctx, tx, userID); err != nil {
		return Group{}, err
	}

	var id uuid.UUID
	var code string
	// Retried on a collision rather than failed: the space is large, but a
	// duplicate must not cost somebody their group.
	for attempt := 0; ; attempt++ {
		if attempt == 5 {
			return Group{}, errors.New("groups: could not find a free code")
		}
		if code, err = invites.NewCode(); err != nil {
			return Group{}, err
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO groups (name, join_code, created_by, created_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (join_code) DO NOTHING
			RETURNING id`, clean, code, userID, now).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return Group{}, fmt.Errorf("groups: creating: %w", err)
		}
		break
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO group_members (group_id, user_id, role, joined_at)
		VALUES ($1, $2, 'admin', $3)`, id, userID, now); err != nil {
		return Group{}, fmt.Errorf("groups: adding the creator: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Group{}, fmt.Errorf("groups: commit: %w", err)
	}
	return Group{ID: id, Name: clean, Role: "admin", Members: 1, Code: code}, nil
}

// Join adds the caller to the group whose current code this is.
//
// Joining a group one is already in is not an error: the code worked, and the
// answer is the group.
func (s *Service) Join(ctx context.Context, userID uuid.UUID, code string) (Group, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	now := s.clock.Now()

	// The ceiling is checked before the code, and a right code over the ceiling
	// is refused too. Otherwise the eleventh guess would still say whether it
	// was right, and the limit would slow the search down without stopping it.
	var failures int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM group_join_failures
		WHERE user_id = $1 AND at > $2`, userID, now.Add(-24*time.Hour)).Scan(&failures); err != nil {
		return Group{}, fmt.Errorf("groups: counting attempts: %w", err)
	}
	if failures >= FailuresPerDay {
		return Group{}, ErrTooManyAttempts
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Group{}, fmt.Errorf("groups: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var groupID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM groups WHERE join_code = $1`, code).Scan(&groupID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Recorded outside the transaction, which is about to roll back.
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO group_join_failures (user_id, at) VALUES ($1, $2)`, userID, now); err != nil {
			return Group{}, fmt.Errorf("groups: recording the attempt: %w", err)
		}
		return Group{}, ErrCodeNotUsable
	}
	if err != nil {
		return Group{}, fmt.Errorf("groups: looking up the code: %w", err)
	}

	var already bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM group_members
		               WHERE group_id = $1 AND user_id = $2 AND left_at IS NULL)`,
		groupID, userID).Scan(&already); err != nil {
		return Group{}, fmt.Errorf("groups: checking membership: %w", err)
	}
	if !already {
		if err := s.roomForOneMore(ctx, tx, userID); err != nil {
			return Group{}, err
		}
		// ON CONFLICT on the partial unique index: two taps on "join" racing
		// each other are one membership, not an error.
		if _, err := tx.Exec(ctx, `
			INSERT INTO group_members (group_id, user_id, role, joined_at)
			VALUES ($1, $2, 'member', $3)
			ON CONFLICT (group_id, user_id) WHERE left_at IS NULL DO NOTHING`,
			groupID, userID, now); err != nil {
			return Group{}, fmt.Errorf("groups: joining: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Group{}, fmt.Errorf("groups: commit: %w", err)
	}
	return s.one(ctx, userID, groupID)
}

// Leave ends the caller's membership.
//
// An admin leaving a group that still has people in it hands the role to
// whoever has been a member longest, rather than being refused. Refusing would
// need a way to appoint somebody else, and a group nobody can administer —
// nobody can see the code or pull the export — is the outcome to avoid either
// way.
func (s *Service) Leave(ctx context.Context, userID, groupID uuid.UUID) error {
	now := s.clock.Now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("groups: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The group row is the lock. Two admins leaving at once would otherwise
	// each see the other as the admin who stays.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM groups WHERE id = $1 FOR UPDATE`, groupID); err != nil {
		return fmt.Errorf("groups: locking: %w", err)
	}

	var role string
	err = tx.QueryRow(ctx, `
		UPDATE group_members SET left_at = $3
		WHERE group_id = $1 AND user_id = $2 AND left_at IS NULL
		RETURNING role`, groupID, userID, now).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotMember
	}
	if err != nil {
		return fmt.Errorf("groups: leaving: %w", err)
	}

	if role == "admin" {
		if _, err := tx.Exec(ctx, `
			UPDATE group_members SET role = 'admin'
			WHERE id = (
				SELECT id FROM group_members
				WHERE group_id = $1 AND left_at IS NULL
				  AND NOT EXISTS (SELECT 1 FROM group_members
				                  WHERE group_id = $1 AND left_at IS NULL AND role = 'admin')
				ORDER BY joined_at, id
				LIMIT 1)`, groupID); err != nil {
			return fmt.Errorf("groups: handing over: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("groups: commit: %w", err)
	}
	return nil
}

// RotateCode replaces the group's code. The old one stops working at once;
// people already in stay in.
func (s *Service) RotateCode(ctx context.Context, userID, groupID uuid.UUID) (string, error) {
	role, err := s.roleOf(ctx, s.pool, userID, groupID)
	if err != nil {
		return "", err
	}
	if role != "admin" {
		return "", ErrNotAdmin
	}
	for attempt := 0; attempt < 5; attempt++ {
		code, err := invites.NewCode()
		if err != nil {
			return "", err
		}
		_, err = s.pool.Exec(ctx, `UPDATE groups SET join_code = $2 WHERE id = $1`, groupID, code)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("groups: replacing the code: %w", err)
		}
		return code, nil
	}
	return "", errors.New("groups: could not find a free code")
}

// Mine lists the groups the caller is in, oldest membership first.
func (s *Service) Mine(ctx context.Context, userID uuid.UUID) ([]Group, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT g.id, g.name, m.role,
		       (SELECT count(*) FROM group_members x WHERE x.group_id = g.id AND x.left_at IS NULL),
		       CASE WHEN m.role = 'admin' THEN g.join_code ELSE '' END
		FROM group_members m JOIN groups g ON g.id = m.group_id
		WHERE m.user_id = $1 AND m.left_at IS NULL
		ORDER BY m.joined_at, g.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("groups: listing: %w", err)
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Role, &g.Members, &g.Code); err != nil {
			return nil, fmt.Errorf("groups: reading: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Service) one(ctx context.Context, userID, groupID uuid.UUID) (Group, error) {
	all, err := s.Mine(ctx, userID)
	if err != nil {
		return Group{}, err
	}
	for _, g := range all {
		if g.ID == groupID {
			return g, nil
		}
	}
	return Group{}, ErrNotMember
}

// querier is what both a pool and a transaction can do.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s *Service) roleOf(ctx context.Context, q querier, userID, groupID uuid.UUID) (string, error) {
	var role string
	err := q.QueryRow(ctx, `
		SELECT role FROM group_members
		WHERE group_id = $1 AND user_id = $2 AND left_at IS NULL`, groupID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotMember
	}
	if err != nil {
		return "", fmt.Errorf("groups: checking membership: %w", err)
	}
	return role, nil
}

// IsMember reports whether userID is a live member of groupID.
func IsMember(ctx context.Context, q querier, userID, groupID uuid.UUID) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM group_members
		               WHERE group_id = $1 AND user_id = $2 AND left_at IS NULL)`,
		groupID, userID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("groups: checking membership: %w", err)
	}
	return ok, nil
}

// SharedNames returns the names of the groups both people are live members
// of, alphabetically. It is the trust signal shown beside a match, so it says
// only what both of them could already see: the group's name, never its code
// or its other members.
func SharedNames(ctx context.Context, pool *pgxpool.Pool, a, b uuid.UUID) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT g.name FROM groups g
		WHERE EXISTS (SELECT 1 FROM group_members m WHERE m.group_id = g.id AND m.user_id = $1 AND m.left_at IS NULL)
		  AND EXISTS (SELECT 1 FROM group_members m WHERE m.group_id = g.id AND m.user_id = $2 AND m.left_at IS NULL)
		ORDER BY g.name`, a, b)
	if err != nil {
		return nil, fmt.Errorf("groups: shared groups: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("groups: shared groups: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Service) roomForOneMore(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	var n int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM group_members WHERE user_id = $1 AND left_at IS NULL`, userID).Scan(&n); err != nil {
		return fmt.Errorf("groups: counting memberships: %w", err)
	}
	if n >= MaxGroupsPerPerson {
		return ErrTooManyGroups
	}
	return nil
}

// Package invites admits new accounts through a code somebody handed out.
//
// It exists for the cold start. A carpooling app in one prefecture lives or
// dies on whether the first strangers trust each other, and the honest signals
// available are thin: a phone number proves a phone, a photo proves nothing.
// A code proves that a person already inside handed it over, which is a claim
// the app issued itself and can therefore stand behind.
package invites

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/domain"

	"carpool/internal/clock"
	"carpool/internal/store"
)

// ErrCodeRequired means the pilot is invite-only and this is a new account.
var ErrCodeRequired = errors.New("invites: an invite code is required")

// ErrCodeNotUsable covers every way a code can fail: unknown, spent, expired or
// withdrawn. They are one error on purpose — telling an outsider which of those
// it was turns the endpoint into an oracle for guessing codes.
var ErrCodeNotUsable = errors.New("invites: that code cannot be used")

// alphabet leaves out the characters people mishear or mistype when a code is
// read out over the phone, which is how most of these will travel.
const alphabet = "ACDEFGHJKLMNPQRTUVWXY34679"

const codeLength = 8

// DefaultUses is how many people one member may bring in.
const DefaultUses = 5

// Service admits accounts and hands out codes.
type Service struct {
	pool  *pgxpool.Pool
	clock clock.Clock
	// required makes a code mandatory for new accounts. Off, everything below
	// still works; it simply is not enforced.
	required bool
}

// NewService returns a service. When required is false, new accounts are
// admitted without a code.
func NewService(pool *pgxpool.Pool, c clock.Clock, required bool) *Service {
	return &Service{pool: pool, clock: c, required: required}
}

// Required reports whether the pilot is invite-only, which decides both
// whether a new account needs a code and whether a link may hand one out.
func (s *Service) Required() bool {
	return s.required
}

// ForLink returns a member's invite code for a link they shared, and whether
// that code can still admit anybody.
//
// It creates the code on first use, because a driver who shares a link has
// vouched for whoever takes it up and must not land on an empty page for a
// step they never took. A spent, expired or withdrawn code comes back with the
// flag down rather than as an error: the page still has something honest to
// say to the stranger. A withdrawn code is never replaced from here; opening a
// public page must not be what hands the driver a fresh one.
func (s *Service) ForLink(ctx context.Context, userID uuid.UUID) (string, bool, error) {
	c, usable, err := s.ensure(ctx, userID, false)
	if err != nil {
		return "", false, err
	}
	return c.Code, usable, nil
}

// Admit signs someone in, creating the account if this phone is new.
//
// The code is checked, the account created and the use spent in one
// transaction. Split apart, two people racing for the last use of a code would
// both pass the check and both get in.
func (s *Service) Admit(
	ctx context.Context, phone, name string, regionID uuid.UUID, born domain.Date, code string,
) (store.User, error) {
	return s.AdmitWith(ctx, phone, name, regionID, born, code, nil)
}

// AdmitWith is Admit with one more thing spent in the same transaction.
//
// The caller that needs this is signup: the ticket proving the phone number was
// verified has to be spent exactly when the account is created, or a ticket can
// buy two accounts, or an account can exist that no ticket paid for. Running it
// after the account is created rather than before means a name the database
// rejects costs nobody their sign-in — the whole transaction rolls back, ticket
// included.
func (s *Service) AdmitWith(
	ctx context.Context, phone, name string, regionID uuid.UUID, born domain.Date, code string,
	alsoSpend func(context.Context, pgx.Tx) error,
) (store.User, error) {
	now := s.clock.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.User{}, fmt.Errorf("invites: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Somebody who already has an account is signing in, not joining. Asking
	// them for a code would lock existing members out the day it is switched on.
	var existing bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM users WHERE phone = $1)`, phone).Scan(&existing); err != nil {
		return store.User{}, fmt.Errorf("invites: looking up the phone: %w", err)
	}

	if !existing && s.required {
		if code == "" {
			return store.User{}, ErrCodeRequired
		}
		if err := s.spend(ctx, tx, code, now); err != nil {
			return store.User{}, err
		}
	}

	user, err := store.EnsureUserTx(ctx, tx, phone, name, regionID, born)
	if err != nil {
		return store.User{}, err
	}

	if !existing && s.required {
		if _, err := tx.Exec(ctx, `
			INSERT INTO invite_redemptions (code, user_id, redeemed_at)
			VALUES ($1, $2, $3)`, code, user.ID, now); err != nil {
			return store.User{}, fmt.Errorf("invites: recording the redemption: %w", err)
		}
	}

	if alsoSpend != nil {
		if err := alsoSpend(ctx, tx); err != nil {
			return store.User{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return store.User{}, fmt.Errorf("invites: commit: %w", err)
	}
	return user, nil
}

// spend takes one use of a code, under a row lock so a concurrent signup cannot
// take the same one.
func (s *Service) spend(ctx context.Context, tx pgx.Tx, code string, now time.Time) error {
	var maxUses, used int
	var expires, disabled *time.Time
	err := tx.QueryRow(ctx, `
		SELECT max_uses, used_count, expires_at, disabled_at
		FROM invite_codes WHERE code = $1 FOR UPDATE`, code).
		Scan(&maxUses, &used, &expires, &disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCodeNotUsable
	}
	if err != nil {
		return fmt.Errorf("invites: loading the code: %w", err)
	}
	if disabled != nil || used >= maxUses || (expires != nil && !now.Before(*expires)) {
		return ErrCodeNotUsable
	}

	if _, err := tx.Exec(ctx,
		`UPDATE invite_codes SET used_count = used_count + 1 WHERE code = $1`, code); err != nil {
		return fmt.Errorf("invites: spending the code: %w", err)
	}
	return nil
}

// Code is one member's invite, as they see it.
type Code struct {
	Code      string
	MaxUses   int
	UsedCount int
}

// Mine returns the caller's code, creating one the first time they ask.
func (s *Service) Mine(ctx context.Context, userID uuid.UUID) (Code, error) {
	c, _, err := s.ensure(ctx, userID, true)
	return c, err
}

// ensure reads a member's code, making one when they have none, or when all
// they have is withdrawn and reissue is set.
//
// Under a lock per member, held to the end of the transaction. A fresh route
// link posted to several chats is fetched by their preview bots at once, and
// read-then-insert outside the lock let each of them make a code of its own.
func (s *Service) ensure(ctx context.Context, userID uuid.UUID, reissue bool) (Code, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Code{}, false, fmt.Errorf("invites: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`,
		"invite_code:"+userID.String()); err != nil {
		return Code{}, false, fmt.Errorf("invites: locking: %w", err)
	}

	// The oldest code still in force, and a withdrawn one only if there is
	// nothing else.
	var (
		c        Code
		disabled bool
		usable   bool
	)
	err = tx.QueryRow(ctx, `
		SELECT code, max_uses, used_count, disabled_at IS NOT NULL,
		       disabled_at IS NULL AND used_count < max_uses
		       AND (expires_at IS NULL OR expires_at > $2)
		FROM invite_codes
		WHERE created_by = $1
		ORDER BY disabled_at IS NOT NULL, created_at LIMIT 1`, userID, s.clock.Now()).
		Scan(&c.Code, &c.MaxUses, &c.UsedCount, &disabled, &usable)
	switch {
	case err == nil && (!disabled || !reissue):
		return c, usable, nil
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return Code{}, false, fmt.Errorf("invites: loading your code: %w", err)
	}

	// Retried rather than failed on a collision: the space is large, but a
	// duplicate must not cost somebody their invite.
	for attempt := 0; attempt < 5; attempt++ {
		candidate, err := NewCode()
		if err != nil {
			return Code{}, false, err
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO invite_codes (code, created_by, max_uses)
			VALUES ($1, $2, $3)
			ON CONFLICT (code) DO NOTHING
			RETURNING code, max_uses, used_count`, candidate, userID, DefaultUses).
			Scan(&c.Code, &c.MaxUses, &c.UsedCount)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return Code{}, false, fmt.Errorf("invites: creating your code: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Code{}, false, fmt.Errorf("invites: commit: %w", err)
		}
		return c, true, nil
	}
	return Code{}, false, errors.New("invites: could not find a free code")
}

// NewCode returns a random code from the unambiguous alphabet.
//
// Exported because a group's join code travels the same way an invite does —
// read out, retyped, pasted into a chat — and should survive it the same way.
func NewCode() (string, error) {
	out := make([]byte, codeLength)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", fmt.Errorf("invites: generating a code: %w", err)
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out), nil
}

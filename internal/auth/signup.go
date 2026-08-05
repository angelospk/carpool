package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
)

// SignupTTL is how long somebody has to finish joining after their number has
// been verified.
//
// Ten minutes is a name and a town, typed once, with room for a phone call in
// the middle. Longer would leave a credential lying around on a handset that
// has been put down; shorter would punish somebody who had to go and ask what
// their village is called.
const SignupTTL = 10 * time.Minute

// ErrNoTicket covers every way a signup ticket can fail: unknown, spent,
// expired, or voided by a fresh code. They are one error because the caller
// does the same thing with all of them — start again — and because telling them
// apart would say more about the account than the holder has proved.
var ErrNoTicket = errors.New("auth: that signup ticket cannot be used")

// SignupService issues and spends the tickets that stand between proving a
// phone number and having an account.
type SignupService struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewSignupService returns a service backed by pool.
func NewSignupService(pool *pgxpool.Pool, c clock.Clock) *SignupService {
	return &SignupService{pool: pool, clock: c}
}

// Issue mints a ticket for a phone number whose code has just been verified.
//
// Any ticket already outstanding for the number is dropped. Somebody who asks
// for a second code has started again, and a half-finished signup on the old
// handset must not be able to complete later.
func (s *SignupService) Issue(ctx context.Context, phone string) (string, time.Time, error) {
	if !e164.MatchString(phone) {
		return "", time.Time{}, fmt.Errorf("auth: phone must be E.164, got %q", phone)
	}

	token, err := newTicket()
	if err != nil {
		return "", time.Time{}, err
	}
	expires := s.clock.Now().Add(SignupTTL)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockPhone(ctx, tx, phone); err != nil {
		return "", time.Time{}, err
	}
	if err := voidTickets(ctx, tx, phone); err != nil {
		return "", time.Time{}, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO signup_tickets (token_hash, phone, expires_at, created_at)
		 VALUES ($1, $2, $3, $4)`,
		hashTicket(token), phone, expires, s.clock.Now()); err != nil {
		return "", time.Time{}, fmt.Errorf("auth: storing signup ticket: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", time.Time{}, fmt.Errorf("auth: commit: %w", err)
	}
	return token, expires, nil
}

// Void drops every outstanding ticket for a number. Requesting a fresh code
// calls it, so that a code and the ticket it would have produced cannot both be
// live at once.
func (s *SignupService) Void(ctx context.Context, phone string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockPhone(ctx, tx, phone); err != nil {
		return err
	}
	if err := voidTickets(ctx, tx, phone); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth: commit: %w", err)
	}
	return nil
}

// lockPhone serialises everything that touches one number's tickets.
//
// Issuing is a delete followed by an insert, and the unique index is on the
// token digest rather than on the phone — so two verifications arriving together
// both clear the table and both insert, and the older ticket outlives the newer
// flow that was supposed to replace it. A ticket is a credential; two live at
// once is one more than intended.
//
// Transaction-scoped, so it is released by the commit or the rollback and no
// path can leak it.
func lockPhone(ctx context.Context, tx pgx.Tx, phone string) error {
	sum := sha256.Sum256([]byte("signup:" + phone))
	key := int64(binary.BigEndian.Uint64(sum[:8]))
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
		return fmt.Errorf("auth: locking the number: %w", err)
	}
	return nil
}

// Peek reads the number a ticket stands for without spending it.
//
// The caller needs the phone before it can create anything, and it must not
// spend the ticket until the account is safely created — otherwise a name that
// fails validation, or a town the app does not know, costs somebody their whole
// sign-in.
func (s *SignupService) Peek(ctx context.Context, token string) (string, error) {
	var phone string
	err := s.pool.QueryRow(ctx, `
		SELECT phone FROM signup_tickets
		WHERE token_hash = $1 AND used_at IS NULL AND expires_at > $2`,
		hashTicket(token), s.clock.Now()).Scan(&phone)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoTicket
	}
	if err != nil {
		return "", fmt.Errorf("auth: reading signup ticket: %w", err)
	}
	return phone, nil
}

// SpendTx marks a ticket used inside the caller's transaction, which is how it
// stays exactly-once: the ticket is spent in the same transaction that creates
// the account, so neither can happen without the other.
//
// The condition is in the UPDATE rather than in a preceding SELECT, so two
// exchanges racing on one ticket cannot both find it unused.
func (s *SignupService) SpendTx(ctx context.Context, tx pgx.Tx, token string) error {
	tag, err := tx.Exec(ctx, `
		UPDATE signup_tickets SET used_at = $2
		WHERE token_hash = $1 AND used_at IS NULL AND expires_at > $2`,
		hashTicket(token), s.clock.Now())
	if err != nil {
		return fmt.Errorf("auth: spending signup ticket: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoTicket
	}
	return nil
}

func voidTickets(ctx context.Context, tx pgx.Tx, phone string) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM signup_tickets WHERE phone = $1 AND used_at IS NULL`, phone); err != nil {
		return fmt.Errorf("auth: voiding signup tickets: %w", err)
	}
	return nil
}

// newTicket returns 32 random bytes, base64url so it survives a JSON body and a
// query string unchanged.
func newTicket() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("auth: generating a signup ticket: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// hashTicket digests a ticket for storage. Unsalted SHA-256 is right here and
// wrong for a password: the input is 32 bytes of entropy this process generated,
// so there is nothing to guess and nothing to rainbow-table.
func hashTicket(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

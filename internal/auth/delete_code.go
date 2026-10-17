package auth

// The fresh proof that DELETE /v1/me asks for.
//
// An access token shows that somebody holds a session, not that the account's
// owner is at the handset. Erasing an account cannot be undone, so it takes one
// more thing: a code sent by SMS to the account's own number, spent by the
// deletion. Modelled on the phone-change code (see phone_change.go) and
// deliberately given its own table and its own limits.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
)

var (
	// ErrDeleteCodeFailed covers a wrong, expired, spent, superseded or locked
	// code, and an account that is not there. One answer: a caller can act on
	// none of them differently.
	ErrDeleteCodeFailed = errors.New("that code is not valid")
	// ErrTooManyDeleteCodes means too many codes were asked for in the last hour.
	ErrTooManyDeleteCodes = errors.New("too many attempts")
)

const (
	// DeleteCodeTTL matches the phone-change code: the person is holding the
	// phone.
	DeleteCodeTTL = 10 * time.Minute
)

// DeleteCodeService issues and spends the code that authorises deleting an
// account.
type DeleteCodeService struct {
	pool  *pgxpool.Pool
	clock clock.Clock
	sms   SMSSender
	key   []byte
	// async sends the code after answering, as for a phone change.
	async bool
}

func NewDeleteCodeService(pool *pgxpool.Pool, c clock.Clock, sms SMSSender, key []byte) *DeleteCodeService {
	return &DeleteCodeService{pool: pool, clock: c, sms: sms, key: key}
}

// WithAsyncSMS returns a copy of s that sends the code in the background,
// after the request has been answered.
func (s *DeleteCodeService) WithAsyncSMS() *DeleteCodeService {
	out := *s
	out.async = true
	return &out
}

// Request sends a fresh code to the account's number and returns it, for a
// DEV_MODE caller to display; every other caller must discard it, and the HTTP
// layer is where that is enforced. An earlier unspent code is superseded.
func (s *DeleteCodeService) Request(ctx context.Context, userID uuid.UUID) (string, error) {
	now := s.clock.Now()

	code, err := newCode()
	if err != nil {
		return "", err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("auth: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialises the count below against the insert, for the same reason as in
	// a phone change: counted outside it, two requests arriving together both
	// pass.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`,
		"delete_code:"+userID.String()); err != nil {
		return "", fmt.Errorf("auth: locking: %w", err)
	}

	var phone string
	err = tx.QueryRow(ctx,
		`SELECT phone FROM users WHERE id = $1 AND status = 'active'`, userID).Scan(&phone)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrDeleteCodeFailed
	}
	if err != nil {
		return "", fmt.Errorf("auth: reading the account: %w", err)
	}

	var recent int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM account_delete_codes WHERE user_id = $1 AND created_at > $2`,
		userID, now.Add(-time.Hour)).Scan(&recent); err != nil {
		return "", fmt.Errorf("auth: counting recent requests: %w", err)
	}
	if recent >= maxPerUserPerHour {
		return "", ErrTooManyDeleteCodes
	}

	if _, err := tx.Exec(ctx, `
		WITH superseded AS (
			UPDATE account_delete_codes SET consumed_at = $3
			 WHERE user_id = $1 AND consumed_at IS NULL
		)
		INSERT INTO account_delete_codes (user_id, phone, code_hmac, expires_at, created_at)
		VALUES ($1, $5, $2, $4, $3)`,
		userID, s.digest(userID, code), now, now.Add(DeleteCodeTTL), phone); err != nil {
		return "", fmt.Errorf("auth: storing the code: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("auth: committing: %w", err)
	}

	message := code + " is your carpool code for deleting your account. It expires in 10 minutes."
	if s.async {
		go func() {
			sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if err := s.sms.Send(sendCtx, phone, message); err != nil {
				slog.Warn("sending an account-deletion code", "error", err)
			}
		}()
		return code, nil
	}
	if err := s.sms.Send(ctx, phone, message); err != nil {
		return "", fmt.Errorf("auth: sending the code: %w", err)
	}
	return code, nil
}

// Spend checks the code and uses it up. A wrong guess is counted, and a code
// that has been guessed at maxConfirmAttempts times is dead even if the next
// guess is right.
func (s *DeleteCodeService) Spend(ctx context.Context, userID uuid.UUID, code string) error {
	return s.SpendThen(ctx, userID, code, nil)
}

// SpendThen is Spend with work done in the same transaction once the code has
// checked out. If then fails, the code is not used up: an erasure that fails
// must not cost the person their code. then may be nil.
func (s *DeleteCodeService) SpendThen(ctx context.Context, userID uuid.UUID, code string,
	then func(ctx context.Context, tx pgx.Tx) error) error {
	now := s.clock.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The account first, then its code: the order a phone change takes them in,
	// so the two cannot wait on each other. Two spends of the same code queue
	// here, and the second finds nothing left to spend.
	var phone string
	err = tx.QueryRow(ctx,
		`SELECT phone FROM users WHERE id = $1 AND status = 'active' FOR UPDATE`, userID).Scan(&phone)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDeleteCodeFailed
	}
	if err != nil {
		return fmt.Errorf("auth: reading the account: %w", err)
	}

	// Only a code sent to the number the account has now: one sent to a number
	// the account has since left must not erase it.
	var (
		id       uuid.UUID
		stored   []byte
		attempts int
	)
	err = tx.QueryRow(ctx, `
		SELECT id, code_hmac, attempts
		  FROM account_delete_codes
		 WHERE user_id = $1 AND phone = $3 AND consumed_at IS NULL AND expires_at > $2
		 ORDER BY created_at DESC
		 LIMIT 1
		 FOR UPDATE`, userID, now, phone).Scan(&id, &stored, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDeleteCodeFailed
	}
	if err != nil {
		return fmt.Errorf("auth: reading the code: %w", err)
	}
	if attempts >= maxConfirmAttempts {
		return ErrDeleteCodeFailed
	}

	if !hmac.Equal(stored, s.digest(userID, code)) {
		// Counted in this transaction and committed, as in a phone change.
		if _, err := tx.Exec(ctx,
			`UPDATE account_delete_codes SET attempts = attempts + 1 WHERE id = $1`, id); err != nil {
			return fmt.Errorf("auth: counting the attempt: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("auth: counting the attempt: %w", err)
		}
		return ErrDeleteCodeFailed
	}

	if _, err := tx.Exec(ctx,
		`UPDATE account_delete_codes SET consumed_at = $2 WHERE id = $1`, id, now); err != nil {
		return fmt.Errorf("auth: consuming the code: %w", err)
	}
	if then != nil {
		if err := then(ctx, tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth: committing: %w", err)
	}
	return nil
}

// digest binds the code to the account it was sent for.
func (s *DeleteCodeService) digest(userID uuid.UUID, code string) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte("delete:" + userID.String() + ":" + code))
	return m.Sum(nil)
}

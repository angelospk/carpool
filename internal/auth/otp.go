package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
)

const (
	// OTPTTL is how long a code stays valid.
	OTPTTL = 5 * time.Minute
	// OTPMaxAttempts bounds guesses against one code. Six digits is only a
	// million possibilities, so the attempt limit is the real defence.
	OTPMaxAttempts = 5
	// OTPMaxPerWindow and OTPRateWindow bound how often codes can be requested
	// for one number, capping both SMS spend and SMS-bombing a stranger.
	OTPMaxPerWindow = 3
	OTPRateWindow   = 15 * time.Minute

	otpDigits = 6
)

// Errors returned by the OTP flow. Both request and verify failures are
// deliberately coarse: a caller must not be able to tell a registered number
// from an unregistered one.
var (
	ErrInvalidOTP  = errors.New("auth: invalid or expired code")
	ErrRateLimited = errors.New("auth: too many code requests")
)

// e164 matches the same shape as the users.phone and otp_codes.phone check
// constraints, so the database can never hold something this rejects.
var e164 = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

// OTPService issues and verifies phone one-time codes.
type OTPService struct {
	pool  *pgxpool.Pool
	clock clock.Clock
	sms   SMSSender
}

// NewOTPService returns a service backed by pool.
func NewOTPService(pool *pgxpool.Pool, c clock.Clock, sms SMSSender) *OTPService {
	return &OTPService{pool: pool, clock: c, sms: sms}
}

// Request generates a code, stores its digest and sends it.
//
// Any previous live code for the number is consumed first, so a resend
// invalidates its predecessor and the partial unique index stays satisfied.
// Request issues a code and sends it. It returns the code so that a caller
// running in DEV_MODE can show it to whoever is testing; every other caller
// must discard it, and the HTTP layer is where that rule is enforced.
func (s *OTPService) Request(ctx context.Context, phone string) (string, error) {
	if !e164.MatchString(phone) {
		return "", fmt.Errorf("auth: phone must be E.164, got %q", phone)
	}
	now := s.clock.Now()

	var recent int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM otp_codes WHERE phone = $1 AND created_at > $2`,
		phone, now.Add(-OTPRateWindow)).Scan(&recent)
	if err != nil {
		return "", fmt.Errorf("auth: counting recent codes: %w", err)
	}
	if recent >= OTPMaxPerWindow {
		return "", ErrRateLimited
	}

	code, err := newCode()
	if err != nil {
		return "", err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("auth: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`UPDATE otp_codes SET consumed_at = $2 WHERE phone = $1 AND consumed_at IS NULL`,
		phone, now); err != nil {
		return "", fmt.Errorf("auth: superseding previous code: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO otp_codes (phone, code_hash, expires_at, created_at)
		 VALUES ($1, $2, $3, $4)`,
		phone, hashCode(phone, code), now.Add(OTPTTL), now); err != nil {
		return "", fmt.Errorf("auth: storing code: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("auth: commit: %w", err)
	}

	// Sent after commit: a delivered code that was never stored is
	// unverifiable, which is the worse failure.
	if err := s.sms.Send(ctx, phone, otpMessage(code)); err != nil {
		return "", fmt.Errorf("auth: sending code: %w", err)
	}
	return code, nil
}

// Verify consumes the live code for phone if it matches.
//
// The row is locked for the whole check so two devices racing on the same code
// cannot both succeed.
func (s *OTPService) Verify(ctx context.Context, phone, code string) error {
	now := s.clock.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		id         string
		storedHash string
		expiresAt  time.Time
		attempts   int
	)
	err = tx.QueryRow(ctx,
		`SELECT id, code_hash, expires_at, attempts
		 FROM otp_codes
		 WHERE phone = $1 AND consumed_at IS NULL
		 FOR UPDATE`, phone).Scan(&id, &storedHash, &expiresAt, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidOTP
	}
	if err != nil {
		return fmt.Errorf("auth: loading code: %w", err)
	}

	if attempts >= OTPMaxAttempts {
		return ErrInvalidOTP
	}
	if now.After(expiresAt) {
		return ErrInvalidOTP
	}

	if hashCode(phone, code) != storedHash {
		if _, err := tx.Exec(ctx,
			`UPDATE otp_codes SET attempts = attempts + 1 WHERE id = $1`, id); err != nil {
			return fmt.Errorf("auth: recording failed attempt: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("auth: commit: %w", err)
		}
		return ErrInvalidOTP
	}

	if _, err := tx.Exec(ctx,
		`UPDATE otp_codes SET consumed_at = $2 WHERE id = $1`, id, now); err != nil {
		return fmt.Errorf("auth: consuming code: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth: commit: %w", err)
	}
	return nil
}

// newCode returns a cryptographically random otpDigits-digit code.
func newCode() (string, error) {
	max := big.NewInt(1)
	for range otpDigits {
		max.Mul(max, big.NewInt(10))
	}
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("auth: generating code: %w", err)
	}
	return fmt.Sprintf("%0*d", otpDigits, n), nil
}

// hashCode binds the digest to the phone number, so a digest observed for one
// number cannot be replayed against another.
func hashCode(phone, code string) string {
	sum := sha256.Sum256([]byte(phone + ":" + code))
	return hex.EncodeToString(sum[:])
}

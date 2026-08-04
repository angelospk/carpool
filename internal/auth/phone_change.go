package auth

// Moving an account to a different phone number.
//
// The number is the credential here: it is what a sign-in proves control of.
// So this is not a profile field with an extra confirmation bolted on, it is a
// change of credential, and it is written as one.
//
// Three things shape it.
//
// The code proves control of the new number, and nothing more. It cannot prove
// the person holding the session is the account's owner, because in this app
// there is no second factor to ask. What bounds that is the access token's
// fifteen minutes: an attacker needs a live session, not a stale one. Said out
// loud because it is a real limit, not an oversight.
//
// Confirmation quotes a challenge id, never a phone number. If the caller could
// name the target at confirmation time, a code sent to a number they control
// could be spent against a number they do not.
//
// And the swap revokes every refresh token. Otherwise somebody holding a
// ninety-day token from before the change keeps the account after the thing
// that identifies it has changed hands.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
)

var (
	// ErrPhoneTaken covers both "somebody else has it" and losing the race to
	// claim it. One answer, because a caller can act on neither differently and
	// telling them apart says which numbers are registered.
	ErrPhoneTaken = errors.New("that number is already in use")
	// ErrSameNumber is separate: it is a mistake rather than a refusal, and the
	// person can see why immediately.
	ErrSameNumber      = errors.New("that is already your number")
	ErrChallengeFailed = errors.New("that code is not valid")
	ErrTooManyChanges  = errors.New("too many attempts")
)

const (
	// PhoneChangeTTL is shorter than a sign-in code's life. The person is
	// holding the phone; they are not coming back to this tomorrow.
	PhoneChangeTTL = 10 * time.Minute
	// maxConfirmAttempts before a challenge is dead. Six digits with five
	// guesses is one in two hundred thousand.
	maxConfirmAttempts = 5
	// Separate buckets from sign-in, deliberately. Sharing one lets ordinary
	// sign-in traffic block a change, and lets an authenticated user send SMS
	// to any number they like by repeatedly asking to move to it.
	maxPerUserPerHour   = 5
	maxPerTargetPerHour = 3
)

// PhoneChangeService moves an account to a new number.
type PhoneChangeService struct {
	pool     *pgxpool.Pool
	clock    clock.Clock
	sms      SMSSender
	sessions *SessionService
	// key is what makes the stored digest useless on its own. Six digits is a
	// million possibilities, which a laptop walks through in seconds against a
	// bare SHA-256 of a leaked table.
	key []byte
}

func NewPhoneChangeService(pool *pgxpool.Pool, c clock.Clock, sms SMSSender, sessions *SessionService, key []byte) *PhoneChangeService {
	return &PhoneChangeService{pool: pool, clock: c, sms: sms, sessions: sessions, key: key}
}

// Request sends a code to the new number and returns the challenge to quote
// back. The code is returned too, for a DEV_MODE caller to display; every other
// caller must discard it, and the HTTP layer is where that is enforced.
func (s *PhoneChangeService) Request(ctx context.Context, userID uuid.UUID, newPhone string) (uuid.UUID, string, error) {
	if !e164.MatchString(newPhone) {
		return uuid.Nil, "", ErrInvalidOTP
	}
	now := s.clock.Now()

	var currentPhone string
	if err := s.pool.QueryRow(ctx,
		`SELECT phone FROM users WHERE id = $1 AND status = 'active'`, userID).Scan(&currentPhone); err != nil {
		return uuid.Nil, "", fmt.Errorf("auth: reading the account: %w", err)
	}
	if currentPhone == newPhone {
		return uuid.Nil, "", ErrSameNumber
	}

	// Checked before an SMS is sent, so the person is not asked for a code that
	// can never work. Not a substitute for the unique constraint at the end:
	// two people can pass this check at the same moment.
	var taken bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM users WHERE phone = $1 AND status <> 'deleted')`, newPhone).
		Scan(&taken); err != nil {
		return uuid.Nil, "", fmt.Errorf("auth: checking the number: %w", err)
	}
	if taken {
		return uuid.Nil, "", ErrPhoneTaken
	}

	if err := s.withinLimits(ctx, userID, newPhone, now); err != nil {
		return uuid.Nil, "", err
	}

	code, err := newCode()
	if err != nil {
		return uuid.Nil, "", err
	}

	var id uuid.UUID
	err = s.pool.QueryRow(ctx, `
		WITH superseded AS (
			-- A resend replaces its predecessor rather than adding to it, so
			-- there is never a set of live codes to guess against.
			UPDATE phone_change_challenges SET consumed_at = $5
			 WHERE user_id = $1 AND consumed_at IS NULL AND expires_at > $5
		)
		INSERT INTO phone_change_challenges (user_id, old_phone, new_phone, code_hmac, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $6, $5)
		RETURNING id`,
		userID, currentPhone, newPhone, s.digest(newPhone, code), now, now.Add(PhoneChangeTTL)).Scan(&id)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("auth: storing the challenge: %w", err)
	}

	if err := s.sms.Send(ctx, newPhone, code+" is your carpool verification code. It expires in 10 minutes."); err != nil {
		return uuid.Nil, "", fmt.Errorf("auth: sending the code: %w", err)
	}
	return id, code, nil
}

// Confirm swaps the number and returns a fresh token pair.
//
// Everything happens in one transaction under a lock on the account, so a
// refresh arriving at the same moment cannot mint a token that outlives the
// revocation below.
func (s *PhoneChangeService) Confirm(ctx context.Context, userID, challengeID uuid.UUID, code string) (TokenPair, error) {
	now := s.clock.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TokenPair{}, fmt.Errorf("auth: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentPhone string
	if err := tx.QueryRow(ctx,
		`SELECT phone FROM users WHERE id = $1 AND status = 'active' FOR UPDATE`, userID).
		Scan(&currentPhone); err != nil {
		return TokenPair{}, ErrChallengeFailed
	}

	var (
		oldPhone, newPhone string
		stored             []byte
		attempts           int
	)
	err = tx.QueryRow(ctx, `
		SELECT old_phone, new_phone, code_hmac, attempts
		  FROM phone_change_challenges
		 WHERE id = $1 AND user_id = $2 AND consumed_at IS NULL AND expires_at > $3
		 FOR UPDATE`, challengeID, userID, now).Scan(&oldPhone, &newPhone, &stored, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		// Unknown, somebody else's, already spent or expired: all one answer.
		return TokenPair{}, ErrChallengeFailed
	}
	if err != nil {
		return TokenPair{}, fmt.Errorf("auth: reading the challenge: %w", err)
	}

	// The account must not have moved since the challenge was made. Two changes
	// started at once would otherwise both apply, and the second would move a
	// number the first had already given away.
	if oldPhone != currentPhone {
		return TokenPair{}, ErrChallengeFailed
	}

	if attempts >= maxConfirmAttempts {
		return TokenPair{}, ErrTooManyChanges
	}

	if !hmac.Equal(stored, s.digest(newPhone, code)) {
		// Counted inside this transaction and committed, rather than from a
		// second connection: the row above is held under FOR UPDATE, so a
		// separate connection updating it waits for a transaction that is
		// waiting for it. The first version of this deadlocked, and a test hung
		// rather than failed.
		if _, err := tx.Exec(ctx,
			`UPDATE phone_change_challenges SET attempts = attempts + 1 WHERE id = $1`, challengeID); err != nil {
			return TokenPair{}, fmt.Errorf("auth: counting the attempt: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return TokenPair{}, fmt.Errorf("auth: counting the attempt: %w", err)
		}
		return TokenPair{}, ErrChallengeFailed
	}

	if _, err := tx.Exec(ctx,
		`UPDATE users SET phone = $2, updated_at = $3 WHERE id = $1`, userID, newPhone, now); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// The final word on uniqueness. Two people confirming the same
			// number at the same moment both pass the earlier check; exactly
			// one gets here.
			return TokenPair{}, ErrPhoneTaken
		}
		return TokenPair{}, fmt.Errorf("auth: changing the number: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE phone_change_challenges SET consumed_at = $2 WHERE id = $1`, challengeID, now); err != nil {
		return TokenPair{}, fmt.Errorf("auth: consuming the challenge: %w", err)
	}

	// Every session, including this one. A refresh token from before the change
	// would otherwise still open an account whose identifier has moved, and the
	// person doing the change is right here to sign in again.
	if _, err := tx.Exec(ctx, `DELETE FROM refresh_tokens WHERE user_id = $1`, userID); err != nil {
		return TokenPair{}, fmt.Errorf("auth: revoking sessions: %w", err)
	}

	// Any sign-in code outstanding for either number is dead too: one sent to
	// the old number would otherwise still be spendable by whoever holds it now.
	if _, err := tx.Exec(ctx,
		`UPDATE otp_codes SET consumed_at = $3 WHERE phone IN ($1, $2) AND consumed_at IS NULL`,
		oldPhone, newPhone, now); err != nil {
		return TokenPair{}, fmt.Errorf("auth: clearing codes: %w", err)
	}

	// Issued inside the same transaction as the revocation above, so there is
	// no instant where the account has no way in and no instant where an old
	// token and a new one are both live.
	token, err := NewRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}
	pair, _, err := s.sessions.issue(ctx, tx, userID, uuid.New(), token)
	if err != nil {
		return TokenPair{}, fmt.Errorf("auth: starting a session: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return TokenPair{}, fmt.Errorf("auth: committing: %w", err)
	}

	// After the commit, and its failure is not the change's failure: the number
	// has moved either way, and the old handset may be exactly what is gone.
	// Deliberately says nothing about the account.
	_ = s.sms.Send(ctx, oldPhone, "The carpool account that used this number has moved to another number.")

	return pair, nil
}

func (s *PhoneChangeService) withinLimits(ctx context.Context, userID uuid.UUID, target string, now time.Time) error {
	since := now.Add(-time.Hour)

	var byUser, byTarget int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE user_id = $1),
		       count(*) FILTER (WHERE new_phone = $2)
		  FROM phone_change_challenges
		 WHERE created_at > $3`, userID, target, since).Scan(&byUser, &byTarget); err != nil {
		return fmt.Errorf("auth: counting recent requests: %w", err)
	}
	if byUser >= maxPerUserPerHour || byTarget >= maxPerTargetPerHour {
		return ErrTooManyChanges
	}
	return nil
}

// digest binds the code to the number it was sent to, so a digest seen for one
// target cannot be replayed against another.
func (s *PhoneChangeService) digest(phone, code string) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(phone + ":" + code))
	return m.Sum(nil)
}

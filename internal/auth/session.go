package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
)

// ErrInvalidRefresh covers every refresh failure — unknown, expired, revoked or
// replayed. The reason is deliberately not disclosed to the caller.
var ErrInvalidRefresh = errors.New("auth: invalid refresh token")

// TokenPair is what a client receives on login or refresh.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// SessionService issues, rotates and revokes sessions.
type SessionService struct {
	pool   *pgxpool.Pool
	clock  clock.Clock
	issuer *TokenIssuer
}

// NewSessionService returns a service backed by pool.
func NewSessionService(pool *pgxpool.Pool, c clock.Clock, issuer *TokenIssuer) *SessionService {
	return &SessionService{pool: pool, clock: c, issuer: issuer}
}

// Create starts a new session. Each call begins its own token family, so
// sessions on different devices are independent.
func (s *SessionService) Create(ctx context.Context, userID uuid.UUID) (TokenPair, error) {
	token, err := NewRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}
	pair, _, err := s.issue(ctx, s.pool, userID, uuid.New(), token)
	return pair, err
}

// Refresh exchanges a refresh token for a new pair, rotating the token.
//
// Presenting a token that was already exchanged means two parties hold it, so
// the entire family is revoked rather than only the replayed token: we cannot
// tell the legitimate client from the thief, and leaving the thief's token live
// would be the worse mistake.
//
// A retry is told from a replay by attemptID: the client stores a random id
// before it sends the request and reuses it when it retries, so a spent token
// presented with the id that spent it is the same logical request arriving
// twice, not two parties holding one credential. Such a retry gets the same
// successor back. Any other id — or none — is still treated as a replay.
func (s *SessionService) Refresh(ctx context.Context, refreshToken string, attemptID uuid.UUID) (TokenPair, error) {
	now := s.clock.Now()

	parentHash := HashRefreshToken(refreshToken)

	// The family lock has to be taken before the token row, and the family is
	// only known from the row — hence this unlocked peek. Nothing is decided on
	// it; every value is read again under the lock below.
	var family uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT family_id FROM refresh_tokens WHERE token_hash = $1`, parentHash).Scan(&family)
	if errors.Is(err, pgx.ErrNoRows) {
		return TokenPair{}, ErrInvalidRefresh
	}
	if err != nil {
		return TokenPair{}, fmt.Errorf("auth: loading refresh token: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TokenPair{}, fmt.Errorf("auth: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockFamily(ctx, tx, family); err != nil {
		return TokenPair{}, err
	}

	var (
		id         uuid.UUID
		userID     uuid.UUID
		familyID   uuid.UUID
		expiresAt  time.Time
		usedAt     *time.Time
		revokedAt  *time.Time
		usedBy     *uuid.UUID
		replacedBy *uuid.UUID
	)
	err = tx.QueryRow(ctx, `
		SELECT id, user_id, family_id, expires_at, used_at, revoked_at, attempt_id, replaced_by
		FROM refresh_tokens
		WHERE token_hash = $1
		FOR UPDATE`, parentHash).Scan(
		&id, &userID, &familyID, &expiresAt, &usedAt, &revokedAt, &usedBy, &replacedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return TokenPair{}, ErrInvalidRefresh
	}
	if err != nil {
		return TokenPair{}, fmt.Errorf("auth: loading refresh token: %w", err)
	}

	if usedAt != nil && attemptID != uuid.Nil && usedBy != nil && *usedBy == attemptID {
		// The same request arriving twice. Hand back exactly what the first one
		// minted; minting anything new here would invalidate the answer the
		// client may yet receive.
		pair, err := s.resend(ctx, tx, userID, parentHash, attemptID, replacedBy, now)
		if err != nil {
			return TokenPair{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return TokenPair{}, fmt.Errorf("auth: commit: %w", err)
		}
		return pair, nil
	}

	if usedAt != nil {
		// Replay detected: burn the family down.
		if _, err := tx.Exec(ctx, `
			UPDATE refresh_tokens SET revoked_at = $2
			WHERE family_id = $1 AND revoked_at IS NULL`, familyID, now); err != nil {
			return TokenPair{}, fmt.Errorf("auth: revoking token family: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return TokenPair{}, fmt.Errorf("auth: commit: %w", err)
		}
		return TokenPair{}, ErrInvalidRefresh
	}

	if revokedAt != nil || now.After(expiresAt) {
		return TokenPair{}, ErrInvalidRefresh
	}

	if _, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET used_at = $2, attempt_id = $3 WHERE id = $1`,
		id, now, nullUUID(attemptID)); err != nil {
		return TokenPair{}, fmt.Errorf("auth: marking token used: %w", err)
	}

	// With an attempt id the successor is derived, so a retry can be answered
	// without the plaintext ever being stored. Without one it is random, as
	// before.
	token := s.issuer.DeriveRefreshToken(parentHash, attemptID)
	if attemptID == uuid.Nil {
		token, err = NewRefreshToken()
		if err != nil {
			return TokenPair{}, err
		}
	}

	pair, successorID, err := s.issue(ctx, tx, userID, familyID, token)
	if err != nil {
		return TokenPair{}, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET replaced_by = $2 WHERE id = $1`, id, successorID); err != nil {
		return TokenPair{}, fmt.Errorf("auth: recording the successor: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return TokenPair{}, fmt.Errorf("auth: commit: %w", err)
	}
	return pair, nil
}

// Revoke ends the session the token belongs to. Only that token's family is
// affected, so other devices stay signed in.
func (s *SessionService) Revoke(ctx context.Context, refreshToken string) error {
	var family uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT family_id FROM refresh_tokens WHERE token_hash = $1`,
		HashRefreshToken(refreshToken)).Scan(&family)
	if errors.Is(err, pgx.ErrNoRows) {
		// Logging out with a token we do not know is already the desired state.
		return nil
	}
	if err != nil {
		return fmt.Errorf("auth: loading session: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockFamily(ctx, tx, family); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = $2
		WHERE family_id = $1 AND revoked_at IS NULL`, family, s.clock.Now()); err != nil {
		return fmt.Errorf("auth: revoking session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth: commit: %w", err)
	}
	return nil
}

// familyLockNamespace keeps these advisory locks from colliding with any other
// use of the two-argument lock space.
const familyLockNamespace = int32(0x63706632) // "cpf2"

// lockFamily serialises every mutation of one token family.
//
// Revoking a family updates the rows that exist when the statement runs, so a
// rotation committing alongside it would insert a token the revocation never
// saw — a live credential surviving "the whole family is revoked", which is the
// one outcome replay detection exists to prevent. Serialising is cheaper than
// making the revocation itself race-proof, since a family belongs to a single
// device and is contended only by a client refreshing twice at once.
//
// The lock is always taken *before* the token row. Taking it after would let a
// rotation hold a row and wait for the family while a revocation holds the
// family and waits for that row.
func lockFamily(ctx context.Context, tx pgx.Tx, familyID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`,
		familyLockNamespace, familyID.String()); err != nil {
		return fmt.Errorf("auth: taking family lock: %w", err)
	}
	return nil
}

// querier is satisfied by both *pgxpool.Pool and pgx.Tx, so issue can run
// standalone or inside the refresh transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// resend answers a retried rotation with the successor the original minted.
//
// The successor's plaintext is not stored anywhere, so it is recomputed from
// the server secret and checked against the stored digest — if the two disagree
// the row is not the one this attempt minted, and nothing is handed out. A
// successor that has since been used, revoked or expired means the retry is
// simply late: it fails, but it does not burn the family, because the attempt
// id already proved this is not a second party.
func (s *SessionService) resend(ctx context.Context, tx pgx.Tx, userID uuid.UUID,
	parentHash string, attemptID uuid.UUID, successorID *uuid.UUID, now time.Time,
) (TokenPair, error) {
	if successorID == nil {
		return TokenPair{}, ErrInvalidRefresh
	}

	var (
		hash      string
		expiresAt time.Time
		usedAt    *time.Time
		revokedAt *time.Time
	)
	err := tx.QueryRow(ctx, `
		SELECT token_hash, expires_at, used_at, revoked_at
		FROM refresh_tokens WHERE id = $1 FOR UPDATE`, *successorID).Scan(
		&hash, &expiresAt, &usedAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return TokenPair{}, ErrInvalidRefresh
	}
	if err != nil {
		return TokenPair{}, fmt.Errorf("auth: loading successor token: %w", err)
	}
	if usedAt != nil || revokedAt != nil || !now.Before(expiresAt) {
		return TokenPair{}, ErrInvalidRefresh
	}

	token := s.issuer.DeriveRefreshToken(parentHash, attemptID)
	if HashRefreshToken(token) != hash {
		return TokenPair{}, ErrInvalidRefresh
	}

	access, err := s.issuer.IssueAccess(userID)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:  access,
		RefreshToken: token,
		ExpiresAt:    now.Add(AccessTokenTTL),
	}, nil
}

// nullUUID maps the zero uuid to SQL NULL, so "no attempt id" is stored as
// absence rather than as a value a caller could present.
func nullUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// issue mints a token pair from the given refresh token and persists its digest.
func (s *SessionService) issue(ctx context.Context, q querier, userID, familyID uuid.UUID, refresh string) (TokenPair, uuid.UUID, error) {
	now := s.clock.Now()

	access, err := s.issuer.IssueAccess(userID)
	if err != nil {
		return TokenPair{}, uuid.Nil, err
	}

	var id uuid.UUID
	expiresAt := now.Add(RefreshTokenTTL)
	if err := q.QueryRow(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, family_id, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		userID, HashRefreshToken(refresh), familyID, expiresAt, now).Scan(&id); err != nil {
		return TokenPair{}, uuid.Nil, fmt.Errorf("auth: storing refresh token: %w", err)
	}

	return TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    now.Add(AccessTokenTTL),
	}, id, nil
}

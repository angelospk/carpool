package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	return s.issue(ctx, s.pool, userID, uuid.New())
}

// Refresh exchanges a refresh token for a new pair, rotating the token.
//
// Presenting a token that was already exchanged means two parties hold it, so
// the entire family is revoked rather than only the replayed token: we cannot
// tell the legitimate client from the thief, and leaving the thief's token live
// would be the worse mistake.
func (s *SessionService) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	now := s.clock.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TokenPair{}, fmt.Errorf("auth: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		id        uuid.UUID
		userID    uuid.UUID
		familyID  uuid.UUID
		expiresAt time.Time
		usedAt    *time.Time
		revokedAt *time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT id, user_id, family_id, expires_at, used_at, revoked_at
		FROM refresh_tokens
		WHERE token_hash = $1
		FOR UPDATE`, HashRefreshToken(refreshToken)).Scan(
		&id, &userID, &familyID, &expiresAt, &usedAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return TokenPair{}, ErrInvalidRefresh
	}
	if err != nil {
		return TokenPair{}, fmt.Errorf("auth: loading refresh token: %w", err)
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
		`UPDATE refresh_tokens SET used_at = $2 WHERE id = $1`, id, now); err != nil {
		return TokenPair{}, fmt.Errorf("auth: marking token used: %w", err)
	}

	pair, err := s.issue(ctx, tx, userID, familyID)
	if err != nil {
		return TokenPair{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TokenPair{}, fmt.Errorf("auth: commit: %w", err)
	}
	return pair, nil
}

// Revoke ends the session the token belongs to. Only that token's family is
// affected, so other devices stay signed in.
func (s *SessionService) Revoke(ctx context.Context, refreshToken string) error {
	now := s.clock.Now()
	_, err := s.pool.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = $2
		WHERE family_id = (SELECT family_id FROM refresh_tokens WHERE token_hash = $1)
		  AND revoked_at IS NULL`, HashRefreshToken(refreshToken), now)
	if err != nil {
		return fmt.Errorf("auth: revoking session: %w", err)
	}
	return nil
}

// execer is satisfied by both *pgxpool.Pool and pgx.Tx, so issue can run
// standalone or inside the refresh transaction.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// issue mints a token pair and persists the refresh half.
func (s *SessionService) issue(ctx context.Context, q execer, userID, familyID uuid.UUID) (TokenPair, error) {
	now := s.clock.Now()

	access, err := s.issuer.IssueAccess(userID)
	if err != nil {
		return TokenPair{}, err
	}
	refresh, err := NewRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}

	expiresAt := now.Add(RefreshTokenTTL)
	if _, err := q.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, family_id, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		userID, HashRefreshToken(refresh), familyID, expiresAt, now); err != nil {
		return TokenPair{}, fmt.Errorf("auth: storing refresh token: %w", err)
	}

	return TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    now.Add(AccessTokenTTL),
	}, nil
}

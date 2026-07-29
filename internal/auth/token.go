// Package auth issues and validates the credentials the API accepts: short
// lived access JWTs and long lived opaque refresh tokens.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"carpool/internal/clock"
)

const (
	// AccessTokenTTL is deliberately short: an access token cannot be revoked,
	// so its blast radius is bounded by its lifetime.
	AccessTokenTTL = 15 * time.Minute
	// RefreshTokenTTL matches ARCHITECTURE.md's 90 days.
	RefreshTokenTTL = 90 * 24 * time.Hour

	issuer        = "carpool"
	signingMethod = "HS256"
	// refreshTokenBytes gives 256 bits of entropy.
	refreshTokenBytes = 32
)

// TokenIssuer mints and validates access tokens.
type TokenIssuer struct {
	secret []byte
	clock  clock.Clock
}

// NewTokenIssuer returns an issuer signing with secret.
func NewTokenIssuer(secret string, c clock.Clock) *TokenIssuer {
	return &TokenIssuer{secret: []byte(secret), clock: c}
}

// IssueAccess mints an access token for the given user.
func (i *TokenIssuer) IssueAccess(userID uuid.UUID) (string, error) {
	now := i.clock.Now()
	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		Issuer:    issuer,
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenTTL)),
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.secret)
	if err != nil {
		return "", fmt.Errorf("auth: signing access token: %w", err)
	}
	return signed, nil
}

// ParseAccess validates an access token and returns the user it identifies.
func (i *TokenIssuer) ParseAccess(token string) (uuid.UUID, error) {
	var claims jwt.RegisteredClaims

	_, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return i.secret, nil },
		// Pinning the algorithm is what stops an alg=none or RS256-confusion
		// downgrade from being accepted.
		jwt.WithValidMethods([]string{signingMethod}),
		jwt.WithIssuer(issuer),
		jwt.WithTimeFunc(i.clock.Now),
	)
	if err != nil {
		return uuid.Nil, fmt.Errorf("auth: invalid access token: %w", err)
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, fmt.Errorf("auth: access token subject is not a uuid: %w", err)
	}
	return userID, nil
}

// NewRefreshToken returns a fresh opaque refresh token.
//
// Refresh tokens are random rather than JWTs precisely so they can be revoked:
// only their digest is stored, and rotation on use makes replay detectable.
func NewRefreshToken() (string, error) {
	buf := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generating refresh token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// DeriveRefreshToken returns the refresh token that a given rotation attempt
// mints. It is deterministic, which is the whole point: only the digest of a
// refresh token is stored, so answering a retry with the *same* token would
// otherwise mean keeping the plaintext around. Recomputing it from the server
// secret keeps the database free of live credentials.
//
// The secret is the server's, so possession of the parent token and the attempt
// id alone does not let a client predict its own next token.
func (i *TokenIssuer) DeriveRefreshToken(parentHash string, attemptID uuid.UUID) string {
	mac := hmac.New(sha256.New, i.secret)
	mac.Write([]byte("carpool/refresh-rotation\x00"))
	mac.Write([]byte(parentHash))
	mac.Write([]byte{0})
	mac.Write([]byte(attemptID.String()))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// HashRefreshToken returns the digest stored in place of the raw token, so a
// database leak does not hand out live sessions.
//
// A plain SHA-256 is right here, unlike for passwords: the input is 256 bits of
// machine-generated entropy, so there is nothing to brute-force and no need for
// a slow KDF.
func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/clock"
)

func base64URL(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

const testSecret = "0123456789abcdef0123456789abcdef"

func newIssuer(t *testing.T) (*TokenIssuer, *clock.Fake) {
	t.Helper()
	c := clock.NewFake(time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC))
	return NewTokenIssuer(testSecret, c), c
}

func TestAccessTokenRoundTripsTheUserID(t *testing.T) {
	iss, _ := newIssuer(t)
	userID := uuid.New()

	token, err := iss.IssueAccess(userID)
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}

	got, err := iss.ParseAccess(token)
	if err != nil {
		t.Fatalf("ParseAccess: %v", err)
	}
	if got != userID {
		t.Fatalf("parsed user id = %v, want %v", got, userID)
	}
}

func TestAccessTokenExpiresAfterItsLifetime(t *testing.T) {
	iss, c := newIssuer(t)
	token, err := iss.IssueAccess(uuid.New())
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}

	c.Advance(AccessTokenTTL + time.Second)

	if _, err := iss.ParseAccess(token); err == nil {
		t.Fatal("expected an expired access token to be rejected")
	}
}

func TestAccessTokenIsStillValidJustBeforeExpiry(t *testing.T) {
	iss, c := newIssuer(t)
	token, err := iss.IssueAccess(uuid.New())
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}

	c.Advance(AccessTokenTTL - time.Second)

	if _, err := iss.ParseAccess(token); err != nil {
		t.Fatalf("token should still be valid one second before expiry: %v", err)
	}
}

func TestAccessTokenIsRejectedWhenSignedWithAnotherSecret(t *testing.T) {
	iss, c := newIssuer(t)
	attacker := NewTokenIssuer("ffffffffffffffffffffffffffffffff", c)
	forged, err := attacker.IssueAccess(uuid.New())
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}

	if _, err := iss.ParseAccess(forged); err == nil {
		t.Fatal("a token signed with a different secret must be rejected")
	}
}

func TestAccessTokenRejectsTheNoneAlgorithm(t *testing.T) {
	iss, _ := newIssuer(t)

	// alg=none with the signature stripped — the classic JWT downgrade attack.
	header := base64URL(`{"alg":"none","typ":"JWT"}`)
	claims := base64URL(`{"sub":"` + uuid.New().String() + `","exp":32503680000}`)
	forged := header + "." + claims + "."

	if _, err := iss.ParseAccess(forged); err == nil {
		t.Fatal("a token using alg=none must be rejected")
	}
}

func TestAccessTokenRejectsGarbage(t *testing.T) {
	iss, _ := newIssuer(t)

	for _, tok := range []string{"", "not-a-token", "a.b.c", strings.Repeat("x", 500)} {
		if _, err := iss.ParseAccess(tok); err == nil {
			t.Errorf("expected %q to be rejected", tok)
		}
	}
}

func TestRefreshTokenIsOpaqueAndUnpredictable(t *testing.T) {
	first, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken: %v", err)
	}
	second, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken: %v", err)
	}

	if first == second {
		t.Fatal("two refresh tokens must not be equal")
	}
	if len(first) < 32 {
		t.Fatalf("refresh token is only %d characters; too little entropy", len(first))
	}
}

func TestRefreshTokenHashIsStableAndDoesNotRevealTheToken(t *testing.T) {
	token, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken: %v", err)
	}

	hash := HashRefreshToken(token)

	if hash != HashRefreshToken(token) {
		t.Fatal("hashing the same token twice must produce the same digest")
	}
	if strings.Contains(hash, token) {
		t.Fatal("the digest must not contain the raw token")
	}
	if hash == HashRefreshToken(token+"x") {
		t.Fatal("different tokens must hash differently")
	}
}

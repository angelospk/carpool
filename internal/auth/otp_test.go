package auth_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/auth"
	"carpool/internal/clock"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

const testPhone = "+306900000001"

func newOTP(t *testing.T) (*auth.OTPService, *pgxpool.Pool, *clock.Fake, *auth.FakeSMS) {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	c := clock.NewFake(time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC))
	sms := &auth.FakeSMS{}
	return auth.NewOTPService(pool, c, sms), pool, c, sms
}

func TestRequestSendsACodeBySMS(t *testing.T) {
	svc, _, _, sms := newOTP(t)

	if _, err := svc.Request(context.Background(), testPhone); err != nil {
		t.Fatalf("Request: %v", err)
	}

	sent := sms.Sent()
	if len(sent) != 1 {
		t.Fatalf("got %d messages, want 1", len(sent))
	}
	if sent[0].Phone != testPhone {
		t.Errorf("sent to %q, want %q", sent[0].Phone, testPhone)
	}
	if !strings.Contains(sent[0].Message, sent[0].Code) {
		t.Errorf("message %q should contain the code %q", sent[0].Message, sent[0].Code)
	}
	if len(sent[0].Code) != 6 {
		t.Errorf("code %q should be 6 digits", sent[0].Code)
	}
}

func TestVerifyAcceptsTheCodeThatWasSent(t *testing.T) {
	svc, _, _, sms := newOTP(t)
	ctx := context.Background()
	if _, err := svc.Request(ctx, testPhone); err != nil {
		t.Fatalf("Request: %v", err)
	}

	if err := svc.Verify(ctx, testPhone, sms.LastCode()); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestVerifyRejectsTheWrongCode(t *testing.T) {
	svc, _, _, sms := newOTP(t)
	ctx := context.Background()
	if _, err := svc.Request(ctx, testPhone); err != nil {
		t.Fatalf("Request: %v", err)
	}

	wrong := "000000"
	if wrong == sms.LastCode() {
		wrong = "111111"
	}

	if err := svc.Verify(ctx, testPhone, wrong); !errors.Is(err, auth.ErrInvalidOTP) {
		t.Fatalf("error = %v, want ErrInvalidOTP", err)
	}
}

func TestVerifyIsSingleUse(t *testing.T) {
	svc, _, _, sms := newOTP(t)
	ctx := context.Background()
	if _, err := svc.Request(ctx, testPhone); err != nil {
		t.Fatalf("Request: %v", err)
	}
	code := sms.LastCode()

	if err := svc.Verify(ctx, testPhone, code); err != nil {
		t.Fatalf("first Verify: %v", err)
	}

	if err := svc.Verify(ctx, testPhone, code); !errors.Is(err, auth.ErrInvalidOTP) {
		t.Fatalf("replaying a consumed code returned %v, want ErrInvalidOTP", err)
	}
}

func TestVerifyRejectsAnExpiredCode(t *testing.T) {
	svc, _, c, sms := newOTP(t)
	ctx := context.Background()
	if _, err := svc.Request(ctx, testPhone); err != nil {
		t.Fatalf("Request: %v", err)
	}
	code := sms.LastCode()

	c.Advance(auth.OTPTTL + time.Second)

	if err := svc.Verify(ctx, testPhone, code); !errors.Is(err, auth.ErrInvalidOTP) {
		t.Fatalf("error = %v, want ErrInvalidOTP for an expired code", err)
	}
}

func TestVerifyAcceptsACodeJustBeforeExpiry(t *testing.T) {
	svc, _, c, sms := newOTP(t)
	ctx := context.Background()
	if _, err := svc.Request(ctx, testPhone); err != nil {
		t.Fatalf("Request: %v", err)
	}
	code := sms.LastCode()

	c.Advance(auth.OTPTTL - time.Second)

	if err := svc.Verify(ctx, testPhone, code); err != nil {
		t.Fatalf("a code one second before expiry should be accepted: %v", err)
	}
}

func TestVerifyLocksOutAfterTooManyAttempts(t *testing.T) {
	svc, _, _, sms := newOTP(t)
	ctx := context.Background()
	if _, err := svc.Request(ctx, testPhone); err != nil {
		t.Fatalf("Request: %v", err)
	}
	code := sms.LastCode()

	for i := range auth.OTPMaxAttempts {
		if err := svc.Verify(ctx, testPhone, "999999"); !errors.Is(err, auth.ErrInvalidOTP) {
			t.Fatalf("attempt %d returned %v, want ErrInvalidOTP", i, err)
		}
	}

	// Even the correct code must not work once the attempt budget is spent,
	// otherwise the limit is only a speed bump against brute force.
	if err := svc.Verify(ctx, testPhone, code); err == nil {
		t.Fatal("the correct code was accepted after the attempt limit was exhausted")
	}
}

func TestRequestReplacesAnyPreviousLiveCode(t *testing.T) {
	svc, _, _, sms := newOTP(t)
	ctx := context.Background()
	if _, err := svc.Request(ctx, testPhone); err != nil {
		t.Fatalf("first Request: %v", err)
	}
	first := sms.LastCode()

	if _, err := svc.Request(ctx, testPhone); err != nil {
		t.Fatalf("second Request: %v", err)
	}
	second := sms.LastCode()

	if first == second {
		t.Skip("the two generated codes collided by chance; nothing to assert")
	}
	if err := svc.Verify(ctx, testPhone, first); !errors.Is(err, auth.ErrInvalidOTP) {
		t.Fatalf("the superseded code returned %v, want ErrInvalidOTP", err)
	}
	if err := svc.Verify(ctx, testPhone, second); err != nil {
		t.Fatalf("the newest code should work: %v", err)
	}
}

func TestRequestIsRateLimitedPerPhone(t *testing.T) {
	svc, _, _, _ := newOTP(t)
	ctx := context.Background()

	for i := range auth.OTPMaxPerWindow {
		if _, err := svc.Request(ctx, testPhone); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}

	if _, err := svc.Request(ctx, testPhone); !errors.Is(err, auth.ErrRateLimited) {
		t.Fatalf("error = %v, want ErrRateLimited", err)
	}
}

func TestRateLimitLiftsAfterTheWindow(t *testing.T) {
	svc, _, c, _ := newOTP(t)
	ctx := context.Background()
	for range auth.OTPMaxPerWindow {
		if _, err := svc.Request(ctx, testPhone); err != nil {
			t.Fatalf("Request: %v", err)
		}
	}

	c.Advance(auth.OTPRateWindow + time.Second)

	if _, err := svc.Request(ctx, testPhone); err != nil {
		t.Fatalf("the limit should lift once the window passes: %v", err)
	}
}

func TestRequestRejectsAMalformedPhoneNumber(t *testing.T) {
	svc, _, _, _ := newOTP(t)

	for _, phone := range []string{"", "6900000001", "+30 690 000 0001", "not-a-phone", "+0123"} {
		if _, err := svc.Request(context.Background(), phone); err == nil {
			t.Errorf("Request(%q) should have been rejected", phone)
		}
	}
}

// Two devices racing on the same code must not both succeed: a single-use code
// that verifies twice would let a shared or intercepted SMS create two sessions.
func TestConcurrentVerifyOfTheSameCodeSucceedsOnce(t *testing.T) {
	svc, _, _, sms := newOTP(t)
	ctx := context.Background()
	if _, err := svc.Request(ctx, testPhone); err != nil {
		t.Fatalf("Request: %v", err)
	}
	code := sms.LastCode()

	const racers = 4
	errs := make([]error, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = svc.Verify(ctx, testPhone, code)
		}()
	}
	close(start)
	wg.Wait()

	var succeeded int
	for _, err := range errs {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("%d of %d concurrent verifications succeeded, want exactly 1", succeeded, racers)
	}
}

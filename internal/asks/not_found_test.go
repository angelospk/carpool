package asks_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/asks"
)

// QA 2026-10-17 finding 7: an ask id that does not exist used to answer
// "already been answered", which is false, and differently from an ask that
// belongs to another driver. The service now has a name for it.
func TestAcceptOfAMissingAskIsNotFound(t *testing.T) {
	f := newFixture(t)

	_, err := f.service().Accept(context.Background(), uuid.New(), f.driverID)

	if !errors.Is(err, asks.ErrAskNotFound) {
		t.Fatalf("error = %v, want ErrAskNotFound", err)
	}
}

func TestDeclineOfAMissingAskIsNotFound(t *testing.T) {
	f := newFixture(t)

	err := f.service().Decline(context.Background(), uuid.New(), f.driverID)

	if !errors.Is(err, asks.ErrAskNotFound) {
		t.Fatalf("error = %v, want ErrAskNotFound", err)
	}
}

// Finding 8: once the sweeper has flipped the row to 'expired', the driver who
// answers a late push is told the ask has expired, not that somebody else
// answered it.
func TestAcceptOfAnAskTheSweeperExpiredSaysExpired(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.clock.Advance(asks.DefaultTTL + time.Minute)
	if n, err := f.service().SweepExpired(ctx); err != nil || n != 1 {
		t.Fatalf("SweepExpired = %d, %v; want 1, nil", n, err)
	}
	if got := f.askStatus(f.askID); got != "expired" {
		t.Fatalf("ask status = %q, want expired", got)
	}

	_, err := f.service().Accept(ctx, f.askID, f.driverID)

	if !errors.Is(err, asks.ErrAskExpired) {
		t.Fatalf("error = %v, want ErrAskExpired", err)
	}
}

func TestDeclineOfAnAskTheSweeperExpiredSaysExpired(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.clock.Advance(asks.DefaultTTL + time.Minute)
	if n, err := f.service().SweepExpired(ctx); err != nil || n != 1 {
		t.Fatalf("SweepExpired = %d, %v; want 1, nil", n, err)
	}

	err := f.service().Decline(ctx, f.askID, f.driverID)

	if !errors.Is(err, asks.ErrAskExpired) {
		t.Fatalf("error = %v, want ErrAskExpired", err)
	}
}

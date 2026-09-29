package asks_test

import (
	"context"
	"errors"
	"testing"

	"carpool/internal/asks"
)

// Women drivers only has to reach the offer already on the driver's phone, the
// way a block does: the matcher applies it when the offer is made, and it can
// change before the driver answers.

func TestAcceptRefusesAfterThePassengerAsksForWomenDriversOnly(t *testing.T) {
	f := newFixture(t)
	mustExec(t, f, `UPDATE users SET gender = 'male' WHERE id = $1`, f.driverID)
	mustExec(t, f, `UPDATE users SET gender = 'female', women_only = true WHERE id = $1`, f.passengerID)

	_, err := f.service().Accept(context.Background(), f.askID, f.driverID)
	if !errors.Is(err, asks.ErrAskNotPending) {
		t.Fatalf("Accept: %v, want ErrAskNotPending", err)
	}
}

func TestAcceptRefusesAfterTheDriverIsNoLongerAWoman(t *testing.T) {
	f := newFixture(t)
	mustExec(t, f, `UPDATE users SET gender = 'female', women_only = true WHERE id = $1`, f.passengerID)
	mustExec(t, f, `UPDATE users SET gender = 'other' WHERE id = $1`, f.driverID)

	_, err := f.service().Accept(context.Background(), f.askID, f.driverID)
	if !errors.Is(err, asks.ErrAskNotPending) {
		t.Fatalf("Accept: %v, want ErrAskNotPending", err)
	}
}

func TestAcceptStillWorksForAWomanDriver(t *testing.T) {
	f := newFixture(t)
	mustExec(t, f, `UPDATE users SET gender = 'female' WHERE id = $1`, f.driverID)
	mustExec(t, f, `UPDATE users SET gender = 'female', women_only = true WHERE id = $1`, f.passengerID)

	if _, err := f.service().Accept(context.Background(), f.askID, f.driverID); err != nil {
		t.Fatalf("Accept: %v", err)
	}
}

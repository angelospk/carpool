package moderation_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/moderation"
)

func (w *world) file(reporter, reported uuid.UUID, reason string) {
	w.t.Helper()
	if err := w.svc.File(context.Background(), reporter, reported, reason); err != nil {
		w.t.Fatalf("File: %v", err)
	}
}

// The queue that did not exist. Reports have been written since the first
// migration and read by nothing, which made the report button a place
// complaints went to be forgotten.
func TestTheQueueShowsOpenReportsOldestFirst(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	// A minute apart, because two reports filed in the same instant are ordered
	// by their ids, which are random — the queue would be right and the test
	// would be a coin toss.
	w.file(w.passenger, w.driver, "first")
	w.clock.Advance(time.Minute)
	w.file(w.passenger, w.driver, "second")

	open, err := w.svc.Open(ctx, 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("%d open reports, want 2", len(open))
	}
	if open[0].Reason != "first" {
		t.Errorf("the queue starts with %q; oldest must come first or a busy week buries last Tuesday", open[0].Reason)
	}
	if open[0].ReportedName == "" || open[0].ReporterName == "" {
		t.Error("the queue is a list of uuids with no names on it")
	}
	// One complaint is a disagreement; the fifth is a pattern.
	if open[0].PriorReports != 1 {
		t.Errorf("prior reports = %d, want 1 — the other report against the same person", open[0].PriorReports)
	}
}

// Deciding is the whole point, and deciding to act has to actually act. A
// status of 'actioned' beside an account still carrying passengers is a queue
// that produces paperwork instead of outcomes.
func TestActioningAReportSuspendsTheAccountInTheSameBreath(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.file(w.passenger, w.driver, "drove off with my bag")

	open, err := w.svc.Open(ctx, 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := w.svc.Decide(ctx, open[0].ID, w.moderator, true, "confirmed by the other passengers"); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	if got := w.status(w.driver); got != "suspended" {
		t.Errorf("account status = %q, want suspended", got)
	}
	if got := w.occurrenceStatus(w.tomorrow); got != "cancelled" {
		t.Errorf("tomorrow's departure = %q; the cascade did not run", got)
	}

	var status, resolution string
	var reviewer uuid.UUID
	if err := w.pool.QueryRow(ctx,
		`SELECT status, resolution, reviewed_by FROM reports WHERE id = $1`,
		open[0].ID).Scan(&status, &resolution, &reviewer); err != nil {
		t.Fatalf("reading the report: %v", err)
	}
	if status != "actioned" || reviewer != w.moderator || resolution == "" {
		t.Errorf("report closed as %q by %v with note %q", status, reviewer, resolution)
	}
}

// Dismissing closes the report and touches nothing else. Most complaints are
// two people who did not get on.
func TestDismissingClosesTheReportAndLeavesTheAccountAlone(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.file(w.passenger, w.driver, "was rude")

	open, _ := w.svc.Open(ctx, 10)
	if err := w.svc.Decide(ctx, open[0].ID, w.moderator, false, "one word against another"); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	if got := w.status(w.driver); got != "active" {
		t.Errorf("account status = %q, want active", got)
	}
	remaining, _ := w.svc.Open(ctx, 10)
	if len(remaining) != 0 {
		t.Errorf("%d reports still open after deciding the only one", len(remaining))
	}
}

// Two moderators working the queue will land on the same report. One decision
// wins; the other is told it has been handled rather than quietly overwriting
// somebody else's judgement and suspending an account twice.
func TestTwoModeratorsDecidingAtOnceProduceOneDecision(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.file(w.passenger, w.driver, "dangerous driving")
	open, _ := w.svc.Open(ctx, 10)

	if err := w.svc.Decide(ctx, open[0].ID, w.moderator, false, "spoke to both"); err != nil {
		t.Fatalf("first decision: %v", err)
	}
	err := w.svc.Decide(ctx, open[0].ID, w.moderator, true, "changed my mind")
	if !errors.Is(err, moderation.ErrAlreadyDecided) {
		t.Fatalf("second decision gave %v, want ErrAlreadyDecided", err)
	}

	if got := w.status(w.driver); got != "active" {
		t.Errorf("the second decision took effect anyway: status = %q", got)
	}
}

// One angry person can fill a queue, and a queue nobody can get through
// protects nobody.
func TestOnePersonCannotFloodTheQueue(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	for i := range moderation.ReportsPerDay {
		if err := w.svc.File(ctx, w.passenger, w.driver, fmt.Sprintf("report %d", i)); err != nil {
			t.Fatalf("report %d: %v", i, err)
		}
	}

	err := w.svc.File(ctx, w.passenger, w.driver, "one more")
	if !errors.Is(err, moderation.ErrTooManyReports) {
		t.Fatalf("the %dst report gave %v, want ErrTooManyReports", moderation.ReportsPerDay+1, err)
	}

	// And somebody else is unaffected: the limit is per person, not per queue.
	if err := w.svc.File(ctx, w.moderator, w.driver, "unrelated"); err != nil {
		t.Errorf("a different reporter was refused: %v", err)
	}
}

func TestAReportNeedsAReasonAndSomebodyElseToBeAbout(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if err := w.svc.File(ctx, w.passenger, w.driver, "   "); err == nil {
		t.Error("an empty reason was accepted")
	}
	if err := w.svc.File(ctx, w.passenger, w.passenger, "myself"); err == nil {
		t.Error("somebody reported themselves")
	}
}

// Somebody who files a complaint is owed the knowledge that it went somewhere.
// What they are not owed is the other person's account status.
func TestAReporterSeesWhatBecameOfTheirOwnReports(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.file(w.passenger, w.driver, "left me at the roadside")

	open, _ := w.svc.Open(ctx, 10)
	if err := w.svc.Decide(ctx, open[0].ID, w.moderator, true, "confirmed"); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	mine, err := w.svc.ByReporter(ctx, w.passenger, 10)
	if err != nil {
		t.Fatalf("ByReporter: %v", err)
	}
	if len(mine) != 1 {
		t.Fatalf("%d reports, want 1", len(mine))
	}
	if mine[0].Status != "actioned" {
		t.Errorf("status = %q, want actioned", mine[0].Status)
	}
	if mine[0].ReportedStatus != "" {
		t.Errorf("the reporter was told the other account is %q, which is not theirs to know",
			mine[0].ReportedStatus)
	}
}

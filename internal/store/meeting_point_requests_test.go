package store_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/domain"
	"carpool/internal/store"
)

// Somewhere well inside Αλεξανδρούπολη, and far enough from every harvested
// point not to be treated as one of them.
const (
	proposalLon = 25.8600
	proposalLat = 40.8600
)

func proposal(by uuid.UUID) store.MeetingPointProposal {
	return store.MeetingPointProposal{
		PlaceID:    alexandroupoli,
		Name:       "Πλατεία Κύπρου",
		Kind:       "other",
		Lon:        proposalLon,
		Lat:        proposalLat,
		ProposedBy: by,
	}
}

func proposerPool(t *testing.T, phone string) (*pgxpool.Pool, uuid.UUID) {
	t.Helper()
	pool := pointsPool(t)
	_, id := seedDriver(t, pool, phone)
	return pool, id
}

func TestAProposalIsRecordedAndWaits(t *testing.T) {
	ctx := context.Background()
	pool, user := proposerPool(t, "+306900000801")

	got, err := store.ProposeMeetingPoint(ctx, pool, proposal(user))
	if err != nil {
		t.Fatalf("ProposeMeetingPoint: %v", err)
	}
	if got.Status != "pending" {
		t.Errorf("status = %q, want pending", got.Status)
	}
	if got.PlaceName == "" {
		t.Error("no settlement name; a reviewer would be reading OSM ids")
	}
}

// Nothing anybody proposes is visible to anybody else until it has been looked
// at. A meeting point is a place a stranger is told to stand.
func TestAPendingProposalIsNotOffered(t *testing.T) {
	ctx := context.Background()
	pool, user := proposerPool(t, "+306900000802")
	before, _ := store.MeetingPointsFor(ctx, pool, alexandroupoli)

	if _, err := store.ProposeMeetingPoint(ctx, pool, proposal(user)); err != nil {
		t.Fatalf("ProposeMeetingPoint: %v", err)
	}

	after, err := store.MeetingPointsFor(ctx, pool, alexandroupoli)
	if err != nil {
		t.Fatalf("MeetingPointsFor: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("a pending proposal is being offered: %v", names(after))
	}
}

func TestAnApprovedProposalBecomesOffered(t *testing.T) {
	ctx := context.Background()
	pool, user := proposerPool(t, "+306900000803")
	_, reviewer := seedDriver(t, pool, "+306900000804")

	req, err := store.ProposeMeetingPoint(ctx, pool, proposal(user))
	if err != nil {
		t.Fatalf("ProposeMeetingPoint: %v", err)
	}
	if err := store.DecideMeetingPointRequest(ctx, pool, req.ID, reviewer, true, ""); err != nil {
		t.Fatalf("approving: %v", err)
	}

	got, err := store.MeetingPointsFor(ctx, pool, alexandroupoli)
	if err != nil {
		t.Fatalf("MeetingPointsFor: %v", err)
	}
	var found bool
	for _, p := range got {
		if p.Name == "Πλατεία Κύπρου" {
			found = true
			if !strings.HasPrefix(p.ID, store.CuratedPrefix) {
				t.Errorf("id = %q, want an identity that cannot collide with OSM", p.ID)
			}
		}
	}
	if !found {
		t.Errorf("an approved proposal is not being offered: %v", names(got))
	}
}

func TestARejectedProposalChangesNothing(t *testing.T) {
	ctx := context.Background()
	pool, user := proposerPool(t, "+306900000805")
	_, reviewer := seedDriver(t, pool, "+306900000806")
	before, _ := store.MeetingPointsFor(ctx, pool, alexandroupoli)

	req, _ := store.ProposeMeetingPoint(ctx, pool, proposal(user))
	if err := store.DecideMeetingPointRequest(ctx, pool, req.ID, reviewer, false, "δεν σταματάει αυτοκίνητο εκεί"); err != nil {
		t.Fatalf("rejecting: %v", err)
	}

	after, _ := store.MeetingPointsFor(ctx, pool, alexandroupoli)
	if len(after) != len(before) {
		t.Errorf("a rejected proposal changed the list: %v", names(after))
	}

	// The reason goes back to the person who asked, so a refusal is an answer
	// rather than silence.
	mine, err := store.MeetingPointRequestsBy(ctx, pool, user, 10)
	if err != nil {
		t.Fatalf("MeetingPointRequestsBy: %v", err)
	}
	if len(mine) != 1 || mine[0].Reason == nil || *mine[0].Reason == "" {
		t.Errorf("the author cannot see why: %+v", mine)
	}
}

// Two moderators reaching for the same request produce one decision.
func TestTwoModeratorsDecidingAtOnceProduceOneDecision(t *testing.T) {
	ctx := context.Background()
	pool, user := proposerPool(t, "+306900000807")
	_, a := seedDriver(t, pool, "+306900000808")
	_, b := seedDriver(t, pool, "+306900000809")

	req, _ := store.ProposeMeetingPoint(ctx, pool, proposal(user))

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, reviewer := range []uuid.UUID{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = store.DecideMeetingPointRequest(ctx, pool, req.ID, reviewer, i == 0, "")
		}()
	}
	wg.Wait()

	var won, lost int
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, store.ErrAlreadyDecided):
			lost++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if won != 1 || lost != 1 {
		t.Errorf("%d decided and %d were told it was taken; want one of each", won, lost)
	}
}

// The queue is other people's business. Somebody's own proposals are their own.
func TestAnAuthorSeesOnlyTheirOwnProposals(t *testing.T) {
	ctx := context.Background()
	pool, mine := proposerPool(t, "+306900000810")
	_, theirs := seedDriver(t, pool, "+306900000811")

	if _, err := store.ProposeMeetingPoint(ctx, pool, proposal(mine)); err != nil {
		t.Fatalf("mine: %v", err)
	}
	other := proposal(theirs)
	other.Name = "Κάπου αλλού"
	if _, err := store.ProposeMeetingPoint(ctx, pool, other); err != nil {
		t.Fatalf("theirs: %v", err)
	}

	got, err := store.MeetingPointRequestsBy(ctx, pool, mine, 10)
	if err != nil {
		t.Fatalf("MeetingPointRequestsBy: %v", err)
	}
	if len(got) != 1 || *got[0].Name != "Πλατεία Κύπρου" {
		t.Errorf("got %d requests: %+v", len(got), got)
	}
}

// A phone that timed out and tried again has not changed its mind.
func TestARetryIsTheSameProposalRatherThanASecond(t *testing.T) {
	ctx := context.Background()
	pool, user := proposerPool(t, "+306900000812")

	p := proposal(user)
	p.IdempotencyKey = "abc-123"

	first, err := store.ProposeMeetingPoint(ctx, pool, p)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := store.ProposeMeetingPoint(ctx, pool, p)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("a retry created a second proposal: %s then %s", first.ID, second.ID)
	}
}

func TestTheDailyLimitHolds(t *testing.T) {
	ctx := context.Background()
	pool, user := proposerPool(t, "+306900000813")

	for i := range store.MaxProposalsPerDay {
		p := proposal(user)
		// Spread out, or the near-duplicate check refuses them first.
		p.Lon = proposalLon + float64(i)*0.01
		if _, err := store.ProposeMeetingPoint(ctx, pool, p); err != nil {
			t.Fatalf("proposal %d: %v", i, err)
		}
	}

	p := proposal(user)
	p.Lon = proposalLon + 0.05
	if _, err := store.ProposeMeetingPoint(ctx, pool, p); !errors.Is(err, store.ErrTooManyProposals) {
		t.Fatalf("error = %v, want ErrTooManyProposals", err)
	}
}

// The limit is a decision about a person, so two requests arriving together
// must not both find room under it.
func TestTheDailyLimitSurvivesTwoAtOnce(t *testing.T) {
	ctx := context.Background()
	pool, user := proposerPool(t, "+306900000814")

	// One place left under the limit.
	for i := range store.MaxProposalsPerDay - 1 {
		p := proposal(user)
		p.Lon = proposalLon + float64(i)*0.01
		if _, err := store.ProposeMeetingPoint(ctx, pool, p); err != nil {
			t.Fatalf("filling up: %v", err)
		}
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := proposal(user)
			p.Lon = proposalLon + 0.03 + float64(i)*0.01
			_, errs[i] = store.ProposeMeetingPoint(ctx, pool, p)
		}()
	}
	wg.Wait()

	var accepted int
	for _, err := range errs {
		if err == nil {
			accepted++
		} else if !errors.Is(err, store.ErrTooManyProposals) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if accepted != 1 {
		t.Errorf("%d proposals got through the last place under the limit", accepted)
	}
}

func TestAProposalIsCheckedBeforeItIsRecorded(t *testing.T) {
	ctx := context.Background()
	pool, user := proposerPool(t, "+306900000815")

	for _, tc := range []struct {
		name    string
		mutate  func(*store.MeetingPointProposal)
		wantErr error
	}{
		{"a settlement that does not exist",
			func(p *store.MeetingPointProposal) { p.PlaceID = "node/999999999999" }, store.ErrUnknownPlace},
		{"a kind nobody offers",
			func(p *store.MeetingPointProposal) { p.Kind = "helipad" }, store.ErrBadKind},
		{"a coordinate in another country",
			func(p *store.MeetingPointProposal) { p.Lon, p.Lat = 2.35, 48.85 }, store.ErrTooFarFromPlace},
		{"a coordinate that is not a number",
			func(p *store.MeetingPointProposal) { p.Lat = 91 }, store.ErrTooFarFromPlace},
		{"a name that is only spaces",
			func(p *store.MeetingPointProposal) { p.Name = "   " }, domain.ErrBadName},
		{"a name carrying a right-to-left override",
			func(p *store.MeetingPointProposal) { p.Name = "Πλατεία\u202e" }, domain.ErrBadName},
	} {
		p := proposal(user)
		tc.mutate(&p)
		if _, err := store.ProposeMeetingPoint(ctx, pool, p); !errors.Is(err, tc.wantErr) {
			t.Errorf("%s: error = %v, want %v", tc.name, err, tc.wantErr)
		}
	}
}

// Somewhere already has a name. Two entries a few metres apart are one place
// with two labels, and a list with both in it is a list somebody has to guess
// from.
func TestAPointOnTopOfAnExistingOneIsRefused(t *testing.T) {
	ctx := context.Background()
	pool, user := proposerPool(t, "+306900000816")

	existing, err := store.MeetingPointsFor(ctx, pool, alexandroupoli)
	if err != nil || len(existing) == 0 {
		t.Fatalf("no harvested points to sit on top of: %v", err)
	}

	p := proposal(user)
	p.Lon, p.Lat = existing[0].Lon, existing[0].Lat
	if _, err := store.ProposeMeetingPoint(ctx, pool, p); !errors.Is(err, store.ErrDuplicatePoint) {
		t.Errorf("error = %v, want ErrDuplicatePoint", err)
	}
}

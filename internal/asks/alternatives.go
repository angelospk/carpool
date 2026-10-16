package asks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"carpool/internal/geo"
	"carpool/internal/match"
)

// ErrNotYourRequest means there is no such request, or it is somebody else's.
// One error for both, so that request ids cannot be probed.
var ErrNotYourRequest = errors.New("asks: no such request")

// Alternative is a departure that could still take a passenger whose ride fell
// through.
//
// It is anonymous on purpose. Nobody on this list has been asked yet, and a
// driver who has not agreed to anything should not have their name and their
// morning handed to a stranger. What the passenger needs from it is whether
// there is still a car on their road today, and roughly when.
type Alternative struct {
	OccurrenceID     uuid.UUID
	DepartEarliest   time.Time
	DepartLatest     time.Time
	OriginLabel      string
	DestinationLabel string
	SeatsFree        int
}

// Alternatives lists the departures the matcher would try next for this
// request, in the order it would try them.
//
// It runs the same two stages AskNext runs, with the same request, the same
// clock and the same ranking, so the list cannot promise a car the dispatcher would
// never approach: blocks, full cars, departed trips, restricted groups and
// drivers already asked are all left out for the same reasons. It asks nobody
// and books nothing — the dispatcher is already working through this list.
func (s *Service) Alternatives(
	ctx context.Context, requestID, passengerID uuid.UUID, limit int,
) ([]Alternative, error) {
	req, err := s.loadRequest(ctx, requestID)
	if errors.Is(err, ErrNoCandidates) || (err == nil && req.PassengerID != passengerID) {
		return nil, ErrNotYourRequest
	}
	if err != nil {
		return nil, err
	}
	// Only while the search is live. A request that was matched, cancelled or
	// ran out of window has nothing left to find.
	if (req.Status != "open" && req.Status != "asking") || req.PassengerStatus != "active" {
		return []Alternative{}, nil
	}

	candidates, err := match.Gross(ctx, s.pool, s.grossQuery(req))
	if err != nil {
		return nil, err
	}
	scored := make([]match.Scored, 0, len(candidates))
	for _, c := range candidates {
		scored = append(scored, match.Scored{ID: c.OccurrenceID.String(), DetourMin: -c.SharedKm})
	}
	byID := make(map[string]match.Candidate, len(candidates))
	for _, c := range candidates {
		byID[c.OccurrenceID.String()] = c
	}

	// Through the routed check too, as AskNext does, so the list does not
	// show a car the dispatcher would then skip: no ordering inside what the
	// driver agreed to, or no road (QA 2026-10-16, B4). Without a router
	// there is no second stage, here or in AskNext.
	ids := make([]uuid.UUID, 0, max(limit, 0))
	for _, r := range match.Rank(scored) {
		if limit > 0 && len(ids) == limit {
			break
		}
		c := byID[r.ID]
		if s.router != nil {
			_, _, _, err := s.evaluate(ctx, c, req)
			if errors.Is(err, match.ErrPartyCapReached) || errors.Is(err, geo.ErrNoRoute) ||
				errors.Is(err, match.ErrNoViableInsertion) {
				continue
			}
			if err != nil {
				return nil, err
			}
		}
		ids = append(ids, c.OccurrenceID)
	}
	if len(ids) == 0 {
		return []Alternative{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT o.id, o.depart_earliest, o.depart_latest, t.origin_label, t.destination_label, o.seats_free
		FROM trip_occurrences o JOIN route_templates t ON t.id = o.template_id
		WHERE o.id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("asks: loading alternatives: %w", err)
	}
	defer rows.Close()
	found := make(map[uuid.UUID]Alternative, len(ids))
	for rows.Next() {
		var a Alternative
		if err := rows.Scan(&a.OccurrenceID, &a.DepartEarliest, &a.DepartLatest,
			&a.OriginLabel, &a.DestinationLabel, &a.SeatsFree); err != nil {
			return nil, fmt.Errorf("asks: reading alternatives: %w", err)
		}
		found[a.OccurrenceID] = a
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("asks: reading alternatives: %w", err)
	}

	out := make([]Alternative, 0, len(ids))
	for _, id := range ids {
		if a, ok := found[id]; ok {
			out = append(out, a)
		}
	}
	return out, nil
}

// grossQuery is a request as the gross filter sees it. One function, so that
// asking the next driver and listing who could be asked cannot drift apart.
func (s *Service) grossQuery(req request) match.GrossQuery {
	return match.GrossQuery{
		Origin:       match.Point{Lon: req.Origin.Lon, Lat: req.Origin.Lat},
		Destination:  match.Point{Lon: req.Destination.Lon, Lat: req.Destination.Lat},
		WindowStart:  req.WindowStart,
		WindowEnd:    req.WindowEnd,
		Seats:        req.Seats,
		Luggage:      req.Luggage,
		LuggageCount: req.LuggageCount,
		RequestKm:    req.RequestKm,
		PassengerID:  req.PassengerID,
		// Both of these used to be applied to the results of this call. The
		// gross stage limits its candidates in SQL, so filtering afterwards
		// discards rows that had already displaced the ones being looked for.
		RequestID: req.ID,
		Target:    req.Target,
		Now:       s.clock.Now(),
	}
}

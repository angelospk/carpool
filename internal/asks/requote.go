package asks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"carpool/internal/domain"
	"carpool/internal/geo"
	"carpool/internal/match"
)

// errPlanMoved means the itinerary changed between routing the re-quotes and
// taking the occurrence lock. Accept starts again from the top.
var errPlanMoved = errors.New("asks: the plan moved while re-quoting")

// acceptAttempts bounds how often Accept starts again because the plan moved
// under it. Each attempt costs a round of OSRM calls.
const acceptAttempts = 3

// quote is what an offer is worth against a plan other than the one it was
// made for.
type quote struct {
	fits bool
	// keep means nothing was routed (no router), so the stored detour and price
	// stand and only the version moves.
	keep                bool
	detourMin, detourKm float64
	// price and basis are nil when the offer carries no receipt to price from;
	// the stored price stands.
	price *float64
	basis []byte
}

// requotePlan is everything Accept needs routed before it takes a lock.
//
// OSRM is never called under a row lock (see Verifier). The plan is routed
// against one itinerary version; Accept compares that version with the one it
// finds under the occurrence lock and starts again if they differ.
type requotePlan struct {
	version int
	// self is the accepted ask against the plan as it stands, set only when the
	// ask was quoted against another version.
	self *quote
	// others are the occurrence's other pending asks against the plan with the
	// accepted passenger aboard. An ask missing here could not be routed (or
	// appeared later): it is left as it is, and re-quoted at its own accept.
	others map[uuid.UUID]quote
	// overCap is set when the accepted passenger fills the last party slot, so
	// no other offer can be taken whatever it costs.
	overCap bool
	// passengers are the people behind the other offers. Accept takes their
	// passenger locks, with its own, in one sorted order before any row lock:
	// re-quoting touches their requests, and two accepts on different days
	// each touching the other's passenger would otherwise deadlock. An offer
	// whose passenger is not here is left alone.
	passengers map[uuid.UUID]bool
}

// lockOrder is the passenger locks Accept takes for plan, its own included,
// sorted so that every Accept takes them in the same order.
func (p requotePlan) lockOrder(own uuid.UUID) []uuid.UUID {
	set := map[uuid.UUID]bool{own: true}
	for id := range p.passengers {
		set[id] = true
	}
	out := make([]uuid.UUID, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

type pendingOffer struct {
	askID, requestID uuid.UUID
	passengerID      uuid.UUID
	version          int
	pickup, dropoff  geo.Point
	tripKm           *float64
}

// planRequotes routes the accepted ask (if stale) and every other pending ask
// on the occurrence against the plan they would join.
func (s *Service) planRequotes(ctx context.Context, occurrenceID, askID uuid.UUID, version int) (requotePlan, error) {
	plan := requotePlan{version: version, others: map[uuid.UUID]quote{}, passengers: map[uuid.UUID]bool{}}

	var budget match.Budget
	if err := s.pool.QueryRow(ctx, `
		SELECT allow_stops, detour_km, detour_min, drive_detour_min
		  FROM trip_occurrences WHERE id = $1`, occurrenceID).
		Scan(&budget.AllowStops, &budget.Km, &budget.Min, &budget.OffRoadMin); err != nil {
		return plan, fmt.Errorf("asks: reading the budget: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.request_id, rr.passenger_id, a.itinerary_version,
		       ST_X(a.pickup_point), ST_Y(a.pickup_point), ST_X(a.dropoff_point), ST_Y(a.dropoff_point),
		       (a.pricing_basis->>'trip_km')::float8
		  FROM asks a JOIN ride_requests rr ON rr.id = a.request_id
		 WHERE a.occurrence_id = $1 AND a.status = 'pending' AND (a.id = $2 OR a.expires_at > $3)
		 ORDER BY a.created_at, a.id`, occurrenceID, askID, s.clock.Now())
	if err != nil {
		return plan, fmt.Errorf("asks: loading pending offers: %w", err)
	}
	var offers []pendingOffer
	var self *pendingOffer
	for rows.Next() {
		var o pendingOffer
		if err := rows.Scan(&o.askID, &o.requestID, &o.passengerID, &o.version,
			&o.pickup.Lon, &o.pickup.Lat, &o.dropoff.Lon, &o.dropoff.Lat, &o.tripKm); err != nil {
			rows.Close()
			return plan, fmt.Errorf("asks: scanning a pending offer: %w", err)
		}
		offers = append(offers, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return plan, fmt.Errorf("asks: reading pending offers: %w", err)
	}
	for i := range offers {
		if offers[i].askID == askID {
			self = &offers[i]
		} else {
			plan.passengers[offers[i].passengerID] = true
		}
	}
	if self == nil {
		// No longer pending; the locked re-read says so with the right error.
		return plan, nil
	}

	itin, err := s.itinerary(ctx, occurrenceID)
	if err != nil {
		return plan, err
	}
	booked := len(itin.Stops) / 2

	if self.version != version {
		q, err := s.quote(ctx, occurrenceID, itin, nil, *self, budget)
		if err != nil {
			return plan, err
		}
		plan.self = &q
	}

	if booked+1 >= match.MaxConcurrentParties {
		plan.overCap = true
		return plan, nil
	}
	after := itin
	after.Stops = append(append([]match.Stop(nil), itin.Stops...),
		match.Stop{Point: self.pickup, Kind: match.StopPickup, PartyID: self.askID.String()},
		match.Stop{Point: self.dropoff, Kind: match.StopDropoff, PartyID: self.askID.String()})
	aboard := &rider{id: self.askID.String(), pickup: self.pickup, dropoff: self.dropoff}
	for _, o := range offers {
		if o.askID == askID {
			continue
		}
		q, err := s.quote(ctx, occurrenceID, after, aboard, o, budget)
		if err != nil {
			// Left quoted against the old plan: its own accept re-quotes it.
			slog.Warn("re-quoting a pending offer", "ask", o.askID, "error", err)
			continue
		}
		plan.others[o.askID] = q
	}
	return plan, nil
}

// quote routes one offer into itin, within the driver's budget, and prices it
// with whoever is in also aboard as well as the bookings.
func (s *Service) quote(
	ctx context.Context, occurrenceID uuid.UUID, itin match.Itinerary, also *rider,
	o pendingOffer, budget match.Budget,
) (quote, error) {
	if s.router == nil {
		return quote{fits: true, keep: true}, nil
	}
	ins, err := match.BestInsertionWithin(ctx, s.router, itin, o.pickup, o.dropoff, budget)
	if errors.Is(err, match.ErrNoViableInsertion) || errors.Is(err, match.ErrPartyCapReached) ||
		errors.Is(err, geo.ErrNoRoute) || errors.Is(err, match.ErrItineraryTooComplex) {
		return quote{fits: false}, nil
	}
	if err != nil {
		return quote{}, err
	}
	q := quote{fits: true, detourMin: ins.DetourMin, detourKm: ins.DetourKm}
	if o.tripKm == nil {
		return q, nil
	}
	pricing, err := s.pricingInputs(ctx, occurrenceID, *o.tripKm)
	if err != nil {
		return quote{}, err
	}
	var extra []rider
	if also != nil {
		extra = append(extra, *also)
	}
	share, err := s.tollShare(ctx, occurrenceID,
		request{ID: o.requestID, Origin: o.pickup, Destination: o.dropoff}, extra...)
	if err != nil {
		return quote{}, err
	}
	pricing.TollShareCents = share
	cents, basis := domain.SuggestedPrice(pricing)
	encoded, err := json.Marshal(basis)
	if err != nil {
		return quote{}, fmt.Errorf("asks: encoding the pricing basis: %w", err)
	}
	price := float64(cents) / 100
	q.price, q.basis = &price, encoded
	return q, nil
}

// applyQuote writes a quote onto a locked ask, against version.
func applyQuote(ctx context.Context, tx pgx.Tx, askID uuid.UUID, q quote, version int) error {
	if q.keep {
		_, err := tx.Exec(ctx, `UPDATE asks SET itinerary_version = $2 WHERE id = $1`, askID, version)
		if err != nil {
			return fmt.Errorf("asks: moving an offer to the new plan: %w", err)
		}
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE asks SET itinerary_version = $2, detour_min = $3, detour_km = $4,
		       suggested_price_eur = COALESCE($5, suggested_price_eur),
		       pricing_basis = COALESCE($6::jsonb, pricing_basis)
		 WHERE id = $1`, askID, version, q.detourMin, q.detourKm, q.price, q.basis); err != nil {
		return fmt.Errorf("asks: re-quoting an offer: %w", err)
	}
	return nil
}

// withdraw ends an offer that can no longer be taken, and puts its passenger
// back in the queue, inside tx. The caller holds the occurrence, request and
// ask locks.
func (s *Service) withdraw(ctx context.Context, tx pgx.Tx, askID, requestID, occurrenceID uuid.UUID, now time.Time) error {
	if _, err := tx.Exec(ctx,
		`UPDATE asks SET status = 'cancelled', responded_at = $2 WHERE id = $1`, askID, now); err != nil {
		return fmt.Errorf("asks: withdrawing an offer: %w", err)
	}
	return s.requeue(ctx, tx, requestID, occurrenceID, "withdrawn")
}

// refuse withdraws the ask being accepted, commits, and returns why. An offer
// the car can no longer take must not stay on the driver's phone holding its
// passenger: that is QA 2026-10-17 F1.
func (s *Service) refuse(ctx context.Context, tx pgx.Tx, askID, requestID, occurrenceID uuid.UUID, now time.Time, why error) (uuid.UUID, error) {
	if err := s.withdraw(ctx, tx, askID, requestID, occurrenceID, now); err != nil {
		return uuid.Nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("asks: commit: %w", err)
	}
	return uuid.Nil, why
}

// requoteOthers moves the occurrence's other pending offers onto the plan the
// accept just made (version), or withdraws the ones it no longer has room for.
// seatsLeft and luggageLeft are what the accept left free. Called with the
// occurrence locked.
func (s *Service) requoteOthers(
	ctx context.Context, tx pgx.Tx, occurrenceID, acceptedAsk uuid.UUID, plan requotePlan,
	version, seatsLeft, luggageLeft int, now time.Time,
) error {
	// A lapsed offer holds nothing: it is the sweeper's, and counting its
	// seats here would withdraw a live offer behind it.
	rows, err := tx.Query(ctx, `
		SELECT a.id, a.request_id, rr.passenger_id FROM asks a
		  JOIN ride_requests rr ON rr.id = a.request_id
		 WHERE a.occurrence_id = $1 AND a.status = 'pending' AND a.id <> $2 AND a.expires_at > $3
		 ORDER BY a.created_at, a.id`, occurrenceID, acceptedAsk, now)
	if err != nil {
		return fmt.Errorf("asks: finding the other offers: %w", err)
	}
	type pair struct{ askID, requestID, passengerID uuid.UUID }
	var others []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.askID, &p.requestID, &p.passengerID); err != nil {
			rows.Close()
			return fmt.Errorf("asks: scanning an offer: %w", err)
		}
		others = append(others, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("asks: reading the other offers: %w", err)
	}

	for _, p := range others {
		if !plan.passengers[p.passengerID] {
			// Made after the plan, so its passenger's lock is not held: its
			// request is not ours to touch. It keeps its seats against the
			// rest and is re-quoted at its own accept.
			var seats, luggage int
			if err := tx.QueryRow(ctx,
				`SELECT seats, luggage_count FROM ride_requests WHERE id = $1`, p.requestID).
				Scan(&seats, &luggage); err != nil {
				return fmt.Errorf("asks: reading an offer's request: %w", err)
			}
			seatsLeft -= seats
			luggageLeft -= luggage
			continue
		}
		// Canonical order: request, then ask.
		var seats, luggage int
		if err := tx.QueryRow(ctx,
			`SELECT seats, luggage_count FROM ride_requests WHERE id = $1 FOR UPDATE`, p.requestID).
			Scan(&seats, &luggage); err != nil {
			return fmt.Errorf("asks: locking an offer's request: %w", err)
		}
		var state string
		if err := tx.QueryRow(ctx, `SELECT status FROM asks WHERE id = $1 FOR UPDATE`, p.askID).
			Scan(&state); err != nil {
			return fmt.Errorf("asks: locking an offer: %w", err)
		}
		if state != "pending" {
			continue
		}
		q, routed := plan.others[p.askID]
		switch {
		case plan.overCap, routed && !q.fits, seats > seatsLeft, luggage > luggageLeft:
			if err := s.withdraw(ctx, tx, p.askID, p.requestID, occurrenceID, now); err != nil {
				return err
			}
		case routed:
			if err := applyQuote(ctx, tx, p.askID, q, version); err != nil {
				return err
			}
			seatsLeft -= seats
			luggageLeft -= luggage
		default:
			// Not routed: stays on the old version until its own accept. It
			// still holds its seats against the rest.
			seatsLeft -= seats
			luggageLeft -= luggage
		}
	}
	return nil
}

package asks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"carpool/internal/domain"
	"carpool/internal/geo"
	"carpool/internal/match"
	"carpool/internal/outbox"
	"carpool/internal/store"
	"carpool/internal/tolls"
	"carpool/internal/vehicles"
)

// ErrNoCandidates means no driver is left to ask for this request.
var ErrNoCandidates = errors.New("asks: no remaining candidates for this request")

// AskNext finds the best driver who has not been asked yet and creates an ask.
//
// It is the single entry point for "approach the next driver", used when a
// request is first created, when an ask is declined, and when one expires. The
// asks(request_id, occurrence_id) unique index is what stops the same driver
// being asked twice after a re-rank or a restart.
func (s *Service) AskNext(ctx context.Context, requestID uuid.UUID) (uuid.UUID, error) {
	req, err := s.loadRequest(ctx, requestID)
	if err != nil {
		return uuid.Nil, err
	}
	if req.Status != "open" {
		// Already matched, cancelled, or an ask is live: nothing to do.
		return uuid.Nil, ErrNoCandidates
	}
	// Refused here as well as in the filter, because this is the cheaper of the
	// two places to find out and it saves an OSRM round trip per candidate.
	if req.PassengerStatus != "active" {
		return uuid.Nil, ErrNoCandidates
	}

	candidates, err := match.Gross(ctx, s.pool, match.GrossQuery{
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
		RequestID: requestID,
		Target:    req.Target,
		Now:       s.clock.Now(),
	})
	if err != nil {
		return uuid.Nil, err
	}

	scored := make([]match.Scored, 0, len(candidates))
	byID := make(map[string]match.Candidate, len(candidates))
	for _, c := range candidates {
		byID[c.OccurrenceID.String()] = c
		scored = append(scored, match.Scored{
			ID: c.OccurrenceID.String(),
			// The gross stage has no routed detour yet; ranking on the shared
			// distance is a cheap proxy that decides which candidates are worth
			// spending OSRM calls on.
			DetourMin: -c.SharedKm,
		})
	}
	if len(scored) == 0 {
		return uuid.Nil, ErrNoCandidates
	}

	for _, cand := range match.Rank(scored) {
		c := byID[cand.ID]

		insertion, price, basis, err := s.evaluate(ctx, c, req)
		if errors.Is(err, match.ErrPartyCapReached) || errors.Is(err, geo.ErrNoRoute) ||
			errors.Is(err, match.ErrNoViableInsertion) {
			// No room, no road, or no ordering inside what this driver agreed
			// to. All three mean the same thing here: try the next occurrence.
			continue
		}
		if err != nil {
			return uuid.Nil, err
		}

		askID, err := s.createAsk(ctx, req, c, insertion, price, basis)
		if errors.Is(err, errAskExists) || errors.Is(err, errNoRoomToAsk) {
			// Raced with another worker, or the seats were spoken for between
			// the filter and the lock. Either way, try the next occurrence.
			continue
		}
		if err != nil {
			return uuid.Nil, err
		}
		return askID, nil
	}

	return uuid.Nil, ErrNoCandidates
}

// request is the subset of a ride request the ask flow needs.
type request struct {
	ID          uuid.UUID
	PassengerID uuid.UUID
	Origin      geo.Point
	Destination geo.Point
	WindowStart time.Time
	WindowEnd   time.Time
	Seats       int
	Status      string
	// PassengerStatus is the account's, not the request's. Somebody suspended
	// between asking and being answered must not have drivers approached on
	// their behalf.
	PassengerStatus string
	// Luggage is what this passenger is bringing, which decides which drivers
	// are worth asking at all.
	Luggage domain.Luggage
	// LuggageCount is how many of them. A driver who takes suitcases and has one
	// place left is not worth asking about three.
	LuggageCount int
	RequestKm    float64
	// Target names the one occurrence a repeat is aimed at. Nil for an ordinary
	// request, which is free to go to whichever driver ranks best.
	Target *uuid.UUID
}

func (s *Service) loadRequest(ctx context.Context, id uuid.UUID) (request, error) {
	var r request
	err := s.pool.QueryRow(ctx, `
		SELECT rr.id, rr.passenger_id,
		       ST_X(rr.origin), ST_Y(rr.origin), ST_X(rr.destination), ST_Y(rr.destination),
		       rr.window_start, rr.window_end, rr.seats, rr.status, rr.target_occurrence_id,
		       ST_Distance(rr.origin::geography, rr.destination::geography) / 1000,
		       rr.luggage, rr.luggage_count, u.status
		FROM ride_requests rr
		JOIN users u ON u.id = rr.passenger_id
		WHERE rr.id = $1`, id).Scan(
		&r.ID, &r.PassengerID,
		&r.Origin.Lon, &r.Origin.Lat, &r.Destination.Lon, &r.Destination.Lat,
		&r.WindowStart, &r.WindowEnd, &r.Seats, &r.Status, &r.Target, &r.RequestKm,
		&r.Luggage, &r.LuggageCount, &r.PassengerStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return request{}, ErrNoCandidates
	}
	if err != nil {
		return request{}, fmt.Errorf("asks: loading request: %w", err)
	}
	return r, nil
}

// The rule this used to implement — a declined driver is never re-asked about
// the same request — now lives in the gross filter's SQL, because it has to be
// applied before the candidate limit rather than to what survives it.

// evaluate runs the fine stage for one candidate: snap the stops, find the best
// insertion, and price it.
func (s *Service) evaluate(ctx context.Context, c match.Candidate, req request) (match.Insertion, int, domain.Basis, error) {
	itin, err := s.itinerary(ctx, c.OccurrenceID)
	if err != nil {
		return match.Insertion{}, 0, domain.Basis{}, err
	}

	// Within what this trip's driver agreed to, rather than the cheapest of all
	// orderings tested against it afterwards: an ordering that fits is worth
	// more to both of them than the one that costs the driver least.
	insertion, err := match.BestInsertionWithin(ctx, s.router, itin, req.Origin, req.Destination, c.Budget)
	if err != nil {
		return match.Insertion{}, 0, domain.Basis{}, err
	}

	pricing, err := s.pricingInputs(ctx, c.OccurrenceID, req.RequestKm)
	if err != nil {
		return match.Insertion{}, 0, domain.Basis{}, err
	}

	// What this passenger owes for barriers is not proportional to anything —
	// it depends on which ones they are actually in the car for. A rider joining
	// after the last toll plaza owes nothing towards it.
	share, err := s.tollShare(ctx, c.OccurrenceID, req)
	if err != nil {
		return match.Insertion{}, 0, domain.Basis{}, err
	}
	pricing.TollShareCents = share

	cents, basis := domain.SuggestedPrice(pricing)
	return insertion, cents, basis, nil
}

// itinerary builds the driver's current plan: their endpoints plus the stops of
// everyone already booked.
func (s *Service) itinerary(ctx context.Context, occurrenceID uuid.UUID) (match.Itinerary, error) {
	var it match.Itinerary
	err := s.pool.QueryRow(ctx, `
		SELECT ST_X(t.origin), ST_Y(t.origin), ST_X(t.destination), ST_Y(t.destination)
		FROM trip_occurrences o JOIN route_templates t ON t.id = o.template_id
		WHERE o.id = $1`, occurrenceID).Scan(
		&it.Origin.Lon, &it.Origin.Lat, &it.Destination.Lon, &it.Destination.Lat)
	if err != nil {
		return match.Itinerary{}, fmt.Errorf("asks: loading itinerary endpoints: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, ST_X(pickup_point), ST_Y(pickup_point),
		           ST_X(dropoff_point), ST_Y(dropoff_point)
		FROM bookings
		WHERE occurrence_id = $1 AND status IN ('confirmed','completed')
		ORDER BY created_at, id`, occurrenceID)
	if err != nil {
		return match.Itinerary{}, fmt.Errorf("asks: loading existing stops: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		var pickup, dropoff geo.Point
		if err := rows.Scan(&id, &pickup.Lon, &pickup.Lat, &dropoff.Lon, &dropoff.Lat); err != nil {
			return match.Itinerary{}, fmt.Errorf("asks: scanning stop: %w", err)
		}
		it.Stops = append(it.Stops,
			match.Stop{Point: pickup, Kind: match.StopPickup, PartyID: id.String()},
			match.Stop{Point: dropoff, Kind: match.StopDropoff, PartyID: id.String()},
		)
	}
	return it, rows.Err()
}

// pricingInputs gathers the driver, car and fuel figures the formula needs.
//
// The fuel price comes from where the trip starts and from what the car burns.
// It used to come from the driver's home region, and from a single number that
// covered petrol and diesel alike. Both were fine while the app ran in one
// prefecture with one figure; nationally the first prices an Αθήνα → Πάτρα run
// off an address in Έβρος, and the second quotes a diesel as though it drank
// petrol on top of a consumption figure that is already fuel-specific.
func (s *Service) pricingInputs(ctx context.Context, occurrenceID uuid.UUID, tripKm float64) (domain.PricingInputs, error) {
	var in domain.PricingInputs
	in.TripKm = tripKm

	var mode string
	var lon, lat float64
	var fuel string
	err := s.pool.QueryRow(ctx, `
		SELECT v.consumption_l_100km, v.consumption_source, d.wear_factor, d.free_below_km,
		       o.pricing_mode, o.seats_total, o.contribution_share,
		       ST_X(t.origin), ST_Y(t.origin), v.fuel
		FROM trip_occurrences o
		JOIN route_templates t ON t.id = o.template_id
		JOIN driver_profiles d ON d.user_id = t.driver_id
		-- The occurrence's car, not the template's and not the driver's current
		-- default. A driver who swaps cars in March must not reprice a trip
		-- somebody booked in February, and a passenger is looking for the car
		-- they were told about.
		JOIN vehicles v ON v.id = o.vehicle_id
		WHERE o.id = $1`, occurrenceID).Scan(
		&in.FuelConsumptionLPer100Km, &in.ConsumptionSource, &in.WearFactor, &in.FreeBelowKm,
		&mode, &in.OfferedSeats, &in.ContributionShare, &lon, &lat, &fuel)
	if err != nil {
		return domain.PricingInputs{}, fmt.Errorf("asks: loading pricing inputs: %w", err)
	}
	in.Mode = domain.PricingMode(mode)

	price, err := store.FuelPriceAt(ctx, s.pool, lon, lat, vehicles.Fuel(fuel))
	if err != nil {
		return domain.PricingInputs{}, fmt.Errorf("asks: pricing fuel at the origin: %w", err)
	}
	in.FuelPriceEUR = price.PriceEUR
	in.Fuel = fuel
	in.RegionName = price.RegionName
	in.PriceIsRegional = price.Regional
	in.PriceSource = price.Source

	return in, nil
}

// tollShare works out this passenger's part of the motorway tolls on the
// driver's route: the barriers between where they get in and where they get
// out, each divided among the people aboard when it is paid.
//
// A route with no tolls, geometry we cannot read, or a station whose price the
// dataset does not know all come back as zero rather than as an error: a
// suggested contribution that is missing its toll component is worth far more
// to a pilot than an ask that never reaches a driver.
func (s *Service) tollShare(ctx context.Context, occurrenceID uuid.UUID, req request) (int, error) {
	if s.tolls == nil {
		return 0, nil
	}

	var encoded string
	err := s.pool.QueryRow(ctx,
		`SELECT ST_AsEncodedPolyline(route_geom, 6) FROM trip_occurrences WHERE id = $1`,
		occurrenceID).Scan(&encoded)
	if err != nil {
		return 0, fmt.Errorf("asks: loading route geometry: %w", err)
	}

	route, err := geo.DecodePolylinePrecision(encoded, 6)
	if err != nil || len(route) < 2 {
		return 0, nil
	}

	gates, priced := s.tolls.Match(route).Gates(tolls.Car)
	if !priced || len(gates) == 0 {
		return 0, nil
	}

	from, to, err := tolls.Segment(route, req.Origin, req.Destination)
	if err != nil {
		// This passenger's own stops do not lie along the route in order. The
		// gross match already refuses such candidates (RISKS 8), and if one ever
		// arrives here the ask is worth more than its toll component.
		return 0, nil
	}

	riders, err := s.ridersAboard(ctx, occurrenceID, route)
	if err != nil {
		return 0, err
	}
	riders = append(riders, tolls.Rider{ID: req.ID.String(), FromMetres: from, ToMetres: to})

	shares, err := tolls.Split(gates, riders)
	if err != nil {
		return 0, nil // a backwards segment is the matcher's problem, not the price's
	}
	return shares.Riders[req.ID.String()], nil
}

// ridersAboard is everyone already booked onto this occurrence, placed along
// the route. The passenger being priced is added by the caller.
func (s *Service) ridersAboard(
	ctx context.Context, occurrenceID uuid.UUID, route []geo.Point,
) ([]tolls.Rider, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, ST_X(pickup_point), ST_Y(pickup_point),
		           ST_X(dropoff_point), ST_Y(dropoff_point)
		FROM bookings
		WHERE occurrence_id = $1 AND status IN ('confirmed','completed')`, occurrenceID)
	if err != nil {
		return nil, fmt.Errorf("asks: loading riders: %w", err)
	}
	defer rows.Close()

	var riders []tolls.Rider
	for rows.Next() {
		var id uuid.UUID
		var pickup, dropoff geo.Point
		if err := rows.Scan(&id, &pickup.Lon, &pickup.Lat, &dropoff.Lon, &dropoff.Lat); err != nil {
			return nil, fmt.Errorf("asks: scanning rider: %w", err)
		}
		from, to, err := tolls.Segment(route, pickup, dropoff)
		if err != nil {
			continue // a booking the geometry no longer supports; not this price's problem
		}
		riders = append(riders, tolls.Rider{ID: id.String(), FromMetres: from, ToMetres: to})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("asks: reading riders: %w", err)
	}
	return riders, nil
}

// errAskExists signals that a pending ask already existed for this request.
var errAskExists = errors.New("asks: an ask already exists")

// errNoRoomToAsk signals that every remaining seat on this occurrence is
// already promised to somebody, so the next candidate should be tried instead.
var errNoRoomToAsk = errors.New("asks: no unspoken-for seat on this occurrence")

// createAsk writes the ask and queues the driver's push, in one transaction.
func (s *Service) createAsk(
	ctx context.Context, req request, c match.Candidate,
	insertion match.Insertion, priceCents int, basis domain.Basis,
) (uuid.UUID, error) {
	// Marshalled here rather than in the SQL so that a basis that cannot be
	// encoded fails loudly instead of storing a null next to a real price.
	encoded, err := json.Marshal(basis)
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: encoding the pricing basis: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Both people, before anything else they own. A status read on its own is a
	// filter and not a gate: suspension can commit between the read and this
	// insert, and the cascade that withdraws somebody's live work has already
	// been past by then, so the ask it did not see survives it. Suspension takes
	// the user row FOR UPDATE first, so sharing it here is what makes the two
	// orders agree and the outcome decided rather than raced.
	if err := lockParticipants(ctx, tx, c.DriverID, req.PassengerID); err != nil {
		if errors.Is(err, errNotActive) || errors.Is(err, errBlocked) {
			// Both mean the same thing to a search: not this driver, try the
			// next one.
			return uuid.Nil, errNoRoomToAsk
		}
		return uuid.Nil, err
	}

	// Lock the occurrence before the request. Accepting an ask takes the
	// occurrence first and touches the request after it, so this is the order
	// that already exists; taking the two the other way round here would let an
	// accept and an ask deadlock against each other.
	//
	// The filter that produced this candidate made the same seat check, but it
	// made it without a lock and possibly seconds ago. Two workers waking two
	// requests for the same newly published trip both pass it and only this
	// recount, under the lock, stops them both writing an offer.
	var seatsFree int
	var departs time.Time
	if err := tx.QueryRow(ctx, `
		SELECT seats_free, depart_earliest FROM trip_occurrences
		WHERE id = $1 AND status = 'scheduled'
		FOR UPDATE`, c.OccurrenceID).Scan(&seatsFree, &departs); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Cancelled or gone between the filter and here. Not this request's
			// problem, and not something retrying fixes.
			return uuid.Nil, errNoRoomToAsk
		}
		return uuid.Nil, fmt.Errorf("asks: locking occurrence: %w", err)
	}

	// Counted in its own statement, after the lock is held, and this separation
	// is load-bearing rather than tidiness. Under READ COMMITTED a SELECT ...
	// FOR UPDATE that waits for another transaction rechecks only the locked
	// row's own conditions when it wakes; a subquery in the same statement keeps
	// the snapshot it started with. Folding the count into the locking query
	// therefore counts the asks as they were before the wait, and two workers
	// racing for the last seat both find it free. Measured, not reasoned about:
	// the concurrency test fails exactly that way when the two are one query.
	var spokenFor int
	if err := tx.QueryRow(ctx,
		`SELECT `+match.SeatsSpokenFor("$1", "$2"), c.OccurrenceID, s.clock.Now()).
		Scan(&spokenFor); err != nil {
		return uuid.Nil, fmt.Errorf("asks: counting offers on the occurrence: %w", err)
	}
	if seatsFree-spokenFor < req.Seats {
		return uuid.Nil, errNoRoomToAsk
	}

	// Lock the request so two workers cannot both create the "next" ask.
	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM ride_requests WHERE id = $1 FOR UPDATE`, req.ID).Scan(&status); err != nil {
		return uuid.Nil, fmt.Errorf("asks: locking request: %w", err)
	}
	if status != "open" {
		return uuid.Nil, errAskExists
	}

	// The ask shows the passenger their own origin and destination. The snapped
	// stops on the driver's road network are recomputed at accept time, against
	// the itinerary as it stands then.
	pickup, dropoff := req.Origin, req.Destination

	// An offer cannot outlive the car it is for. The half hour is how long a
	// driver is given to answer, not a promise that the answer still means
	// something: a trip leaving in ten minutes would otherwise sit on their
	// phone as a live offer for twenty minutes after it left, and hold the
	// passenger's request against a candidate that could only ever have said no.
	// Ending it at the departure hands them back to the sweeper, which reopens
	// the request and looks for somebody who has not gone yet.
	expiresAt := s.clock.Now().Add(DefaultTTL)
	if departs.Before(expiresAt) {
		expiresAt = departs
	}

	var askID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km,
		                  pickup_point, dropoff_point, suggested_price_eur, pricing_basis,
		                  itinerary_version, expires_at)
		VALUES ($1, $2, $3, $4,
		        ST_SetSRID(ST_MakePoint($5, $6), 4326),
		        ST_SetSRID(ST_MakePoint($7, $8), 4326),
		        $9, $10, $11, $12)
		RETURNING id`,
		req.ID, c.OccurrenceID, insertion.DetourMin, insertion.DetourKm,
		pickup.Lon, pickup.Lat, dropoff.Lon, dropoff.Lat,
		float64(priceCents)/100, encoded, c.ItineraryVersion, expiresAt).Scan(&askID)
	if err != nil {
		if isUniqueViolation(err) {
			return uuid.Nil, errAskExists
		}
		return uuid.Nil, fmt.Errorf("asks: creating ask: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE ride_requests SET status = 'asking', updated_at = now() WHERE id = $1`, req.ID); err != nil {
		return uuid.Nil, fmt.Errorf("asks: marking request asking: %w", err)
	}

	payload, err := json.Marshal(map[string]any{
		"ask_id":        askID,
		"occurrence_id": c.OccurrenceID,
		"driver_id":     c.DriverID,
		"detour_min":    insertion.DetourMin,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: encoding ask notification: %w", err)
	}
	if err := outbox.Enqueue(ctx, tx, outbox.Message{
		Kind:           "push",
		IdempotencyKey: "ask:" + askID.String(),
		Payload:        payload,
	}, s.clock.Now()); err != nil {
		return uuid.Nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("asks: commit: %w", err)
	}
	return askID, nil
}

// isUniqueViolation reports whether err is a Postgres unique-constraint error.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

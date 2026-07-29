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

	candidates, err := match.Gross(ctx, s.pool, match.GrossQuery{
		Origin:      match.Point{Lon: req.Origin.Lon, Lat: req.Origin.Lat},
		Destination: match.Point{Lon: req.Destination.Lon, Lat: req.Destination.Lat},
		WindowStart: req.WindowStart,
		WindowEnd:   req.WindowEnd,
		Seats:       req.Seats,
		RequestKm:   req.RequestKm,
		PassengerID: req.PassengerID,
	})
	if err != nil {
		return uuid.Nil, err
	}

	already, err := s.alreadyAsked(ctx, requestID)
	if err != nil {
		return uuid.Nil, err
	}

	scored := make([]match.Scored, 0, len(candidates))
	byID := make(map[string]match.Candidate, len(candidates))
	for _, c := range candidates {
		if _, seen := already[c.OccurrenceID]; seen {
			continue
		}
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

		insertion, price, err := s.evaluate(ctx, c, req)
		if errors.Is(err, match.ErrPartyCapReached) || errors.Is(err, geo.ErrNoRoute) {
			continue // this occurrence cannot take them; try the next
		}
		if err != nil {
			return uuid.Nil, err
		}
		if !insertion.WithinBudget(c.DetourBudget) {
			continue
		}

		askID, err := s.createAsk(ctx, req, c, insertion, price)
		if errors.Is(err, errAskExists) {
			continue // raced with another worker; move on
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
	RequestKm   float64
}

func (s *Service) loadRequest(ctx context.Context, id uuid.UUID) (request, error) {
	var r request
	err := s.pool.QueryRow(ctx, `
		SELECT id, passenger_id,
		       ST_X(origin), ST_Y(origin), ST_X(destination), ST_Y(destination),
		       window_start, window_end, seats, status,
		       ST_Distance(origin::geography, destination::geography) / 1000
		FROM ride_requests WHERE id = $1`, id).Scan(
		&r.ID, &r.PassengerID,
		&r.Origin.Lon, &r.Origin.Lat, &r.Destination.Lon, &r.Destination.Lat,
		&r.WindowStart, &r.WindowEnd, &r.Seats, &r.Status, &r.RequestKm)
	if errors.Is(err, pgx.ErrNoRows) {
		return request{}, ErrNoCandidates
	}
	if err != nil {
		return request{}, fmt.Errorf("asks: loading request: %w", err)
	}
	return r, nil
}

// alreadyAsked returns the occurrences this request has already been offered
// to, in any state. A declined driver is never re-asked about the same request.
func (s *Service) alreadyAsked(ctx context.Context, requestID uuid.UUID) (map[uuid.UUID]struct{}, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT occurrence_id FROM asks WHERE request_id = $1`, requestID)
	if err != nil {
		return nil, fmt.Errorf("asks: loading ask history: %w", err)
	}
	defer rows.Close()

	out := map[uuid.UUID]struct{}{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("asks: scanning ask history: %w", err)
		}
		out[id] = struct{}{}
	}
	return out, rows.Err()
}

// evaluate runs the fine stage for one candidate: snap the stops, find the best
// insertion, and price it.
func (s *Service) evaluate(ctx context.Context, c match.Candidate, req request) (match.Insertion, int, error) {
	itin, err := s.itinerary(ctx, c.OccurrenceID)
	if err != nil {
		return match.Insertion{}, 0, err
	}

	insertion, err := match.BestInsertion(ctx, s.router, itin, req.Origin, req.Destination)
	if err != nil {
		return match.Insertion{}, 0, err
	}

	pricing, err := s.pricingInputs(ctx, c.OccurrenceID, req.RequestKm)
	if err != nil {
		return match.Insertion{}, 0, err
	}
	return insertion, domain.SuggestedPriceCents(pricing), nil
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

// pricingInputs gathers the driver and region figures the fuel formula needs.
func (s *Service) pricingInputs(ctx context.Context, occurrenceID uuid.UUID, tripKm float64) (domain.PricingInputs, error) {
	var in domain.PricingInputs
	in.TripKm = tripKm

	var mode string
	err := s.pool.QueryRow(ctx, `
		SELECT d.fuel_consumption_l_100km, d.wear_factor, d.free_below_km,
		       r.fuel_price_eur, o.pricing_mode,
		       (SELECT count(*) FROM bookings b
		        WHERE b.occurrence_id = o.id AND b.status IN ('confirmed','completed'))
		FROM trip_occurrences o
		JOIN route_templates t ON t.id = o.template_id
		JOIN driver_profiles d ON d.user_id = t.driver_id
		JOIN users u          ON u.id = t.driver_id
		JOIN regions r        ON r.id = u.home_region_id
		WHERE o.id = $1`, occurrenceID).Scan(
		&in.FuelConsumptionLPer100Km, &in.WearFactor, &in.FreeBelowKm,
		&in.FuelPriceEUR, &mode, &in.CurrentPassengers)
	if err != nil {
		return domain.PricingInputs{}, fmt.Errorf("asks: loading pricing inputs: %w", err)
	}
	in.Mode = domain.PricingMode(mode)
	return in, nil
}

// errAskExists signals that a pending ask already existed for this request.
var errAskExists = errors.New("asks: an ask already exists")

// createAsk writes the ask and queues the driver's push, in one transaction.
func (s *Service) createAsk(
	ctx context.Context, req request, c match.Candidate,
	insertion match.Insertion, priceCents int,
) (uuid.UUID, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

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

	var askID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km,
		                  pickup_point, dropoff_point, suggested_price_eur,
		                  itinerary_version, expires_at)
		VALUES ($1, $2, $3, $4,
		        ST_SetSRID(ST_MakePoint($5, $6), 4326),
		        ST_SetSRID(ST_MakePoint($7, $8), 4326),
		        $9, $10, $11)
		RETURNING id`,
		req.ID, c.OccurrenceID, insertion.DetourMin, insertion.DetourKm,
		pickup.Lon, pickup.Lat, dropoff.Lon, dropoff.Lat,
		float64(priceCents)/100, c.ItineraryVersion, s.clock.Now().Add(DefaultTTL)).Scan(&askID)
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
	}); err != nil {
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

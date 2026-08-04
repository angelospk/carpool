package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/domain"
	"carpool/internal/places"
)

// Proposing a meeting point, and deciding about one.
//
// The automatic list is thin outside the towns and wrong inside the cities, and
// the people who know where a car can actually wait are the ones waiting there.
// So they can say so — and nobody sees it until somebody has looked, because a
// meeting point is a place a stranger is told to stand.

var (
	// ErrUnknownPlace means the settlement being proposed for does not exist.
	ErrUnknownPlace = errors.New("store: no such settlement")
	// ErrTooFarFromPlace means the coordinate is not plausibly in that place.
	ErrTooFarFromPlace = errors.New("store: that point is not in that settlement")
	// ErrDuplicatePoint means something is already there under that name.
	ErrDuplicatePoint = errors.New("store: there is already a point there")
	// ErrTooManyProposals means the daily limit is reached.
	ErrTooManyProposals = errors.New("store: too many proposals today")
	// ErrBadKind means the kind is not one of the ones offered.
	ErrBadKind = errors.New("store: that is not a kind of meeting point")
	// ErrAlreadyDecided means somebody else got to this request first.
	ErrAlreadyDecided = errors.New("store: that request has already been decided")
	// ErrNoRequest means there is no such request.
	ErrNoRequest = errors.New("store: no such request")
)

// PointKinds are the kinds a person may choose. "other" exists so that somebody
// who knows the right spot is not stopped by a taxonomy.
var PointKinds = []string{"bus_station", "train_station", "port", "fuel", "parking", "other"}

// MaxProposalsPerDay is how many one person may send. Enough to correct a town
// they know, few enough that filling the queue takes a decision rather than an
// afternoon.
const MaxProposalsPerDay = 3

// nearbyMeters is how close a proposal may be to an existing point before it is
// treated as the same one under a different name.
const nearbyMeters = 75

// plausibleRadiusKm bounds how far from a settlement's centre a point may be.
//
// It is a typo and abuse guard, not a judgement that the point belongs to the
// place — that is what the reviewer is for. The numbers are generous because an
// OSM settlement node is a label position, and in a city it can sit a long way
// from where anybody actually waits.
var plausibleRadiusKm = map[string]float64{"city": 15, "town": 8, "village": 4, "hamlet": 4}

// MeetingPointProposal is a request to add a point, as it comes off the wire.
type MeetingPointProposal struct {
	PlaceID        string
	Name           string
	Kind           string
	Lon, Lat       float64
	ProposedBy     uuid.UUID
	IdempotencyKey string
}

// MeetingPointRequest is a request together with what became of it.
type MeetingPointRequest struct {
	ID        uuid.UUID  `json:"id"`
	PlaceID   string     `json:"place_id"`
	PlaceName string     `json:"place_name"`
	Action    string     `json:"action"`
	TargetID  *string    `json:"target_id,omitempty"`
	Name      *string    `json:"name,omitempty"`
	Kind      *string    `json:"kind,omitempty"`
	Lon       *float64   `json:"lon,omitempty"`
	Lat       *float64   `json:"lat,omitempty"`
	Status    string     `json:"status"`
	Reason    *string    `json:"reason,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	Reviewed  *time.Time `json:"reviewed_at,omitempty"`
}

// ProposeMeetingPoint records somebody's suggestion, pending review.
func ProposeMeetingPoint(ctx context.Context, pool *pgxpool.Pool, p MeetingPointProposal) (MeetingPointRequest, error) {
	place, ok := places.ByID(p.PlaceID)
	if !ok {
		return MeetingPointRequest{}, ErrUnknownPlace
	}

	// The same normalisation a display name gets: this is text other people
	// read next to somewhere they are about to stand, and the invisible
	// formatting runes that make a name render as something else matter more
	// here, not less.
	name, err := domain.NormaliseName(p.Name)
	if err != nil {
		return MeetingPointRequest{}, err
	}

	if !slicesContains(PointKinds, p.Kind) {
		return MeetingPointRequest{}, ErrBadKind
	}
	if err := plausible(place, p.Lon, p.Lat); err != nil {
		return MeetingPointRequest{}, err
	}
	for _, existing := range places.PointsFor(p.PlaceID) {
		if haversineMeters(p.Lon, p.Lat, existing.Lon, existing.Lat) < nearbyMeters {
			return MeetingPointRequest{}, ErrDuplicatePoint
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return MeetingPointRequest{}, fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialised per person, so two requests arriving together cannot both
	// find room under the limit.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`,
		proposalLockNamespace, p.ProposedBy.String()); err != nil {
		return MeetingPointRequest{}, fmt.Errorf("store: taking the proposal lock: %w", err)
	}

	// An idempotent retry is the same proposal, not a second one: a phone that
	// timed out and tried again has not changed its mind.
	if p.IdempotencyKey != "" {
		var existing MeetingPointRequest
		err := tx.QueryRow(ctx, `
			SELECT id, place_id, action, name, kind, status, created_at
			  FROM meeting_point_requests
			 WHERE proposed_by = $1 AND idempotency_key = $2`,
			p.ProposedBy, p.IdempotencyKey).
			Scan(&existing.ID, &existing.PlaceID, &existing.Action, &existing.Name,
				&existing.Kind, &existing.Status, &existing.CreatedAt)
		if err == nil {
			existing.PlaceName = place.Name
			return existing, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return MeetingPointRequest{}, fmt.Errorf("store: checking for a retry: %w", err)
		}
	}

	var today int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM meeting_point_requests
		 WHERE proposed_by = $1 AND created_at > now() - interval '1 day'`,
		p.ProposedBy).Scan(&today); err != nil {
		return MeetingPointRequest{}, fmt.Errorf("store: counting today's proposals: %w", err)
	}
	if today >= MaxProposalsPerDay {
		return MeetingPointRequest{}, ErrTooManyProposals
	}

	var out MeetingPointRequest
	if err := tx.QueryRow(ctx, `
		INSERT INTO meeting_point_requests
		    (place_id, action, name, kind, point, proposed_by, idempotency_key)
		VALUES ($1, 'add', $2, $3, ST_SetSRID(ST_MakePoint($4, $5), 4326), $6, nullif($7, ''))
		RETURNING id, place_id, action, name, kind, status, created_at`,
		p.PlaceID, name, p.Kind, p.Lon, p.Lat, p.ProposedBy, p.IdempotencyKey).
		Scan(&out.ID, &out.PlaceID, &out.Action, &out.Name, &out.Kind, &out.Status, &out.CreatedAt); err != nil {
		return MeetingPointRequest{}, fmt.Errorf("store: recording the proposal: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return MeetingPointRequest{}, fmt.Errorf("store: commit: %w", err)
	}
	out.PlaceName = place.Name
	return out, nil
}

// proposalLockNamespace namespaces the per-person advisory lock.
const proposalLockNamespace int32 = 0x6d70_7472 // "mptr"

// plausible refuses a coordinate that cannot be in that settlement.
func plausible(place places.Place, lon, lat float64) error {
	if math.IsNaN(lon) || math.IsNaN(lat) || math.IsInf(lon, 0) || math.IsInf(lat, 0) {
		return ErrTooFarFromPlace
	}
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return ErrTooFarFromPlace
	}
	radius, ok := plausibleRadiusKm[place.Kind]
	if !ok {
		radius = 4
	}
	if haversineMeters(lon, lat, place.Lon, place.Lat) > radius*1000 {
		return ErrTooFarFromPlace
	}
	return nil
}

func haversineMeters(lon1, lat1, lon2, lat2 float64) float64 {
	const r = 6371000.0
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp := p2 - p1
	dl := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}

func slicesContains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// DecideMeetingPointRequest approves or rejects, and applies the decision.
//
// The decision and its effect are one transaction, and the update is conditional
// on the request still being pending: two moderators reaching for the same
// request produce one decision and one conflict, rather than two decisions and
// whichever effect landed last.
func DecideMeetingPointRequest(ctx context.Context, pool *pgxpool.Pool, id, reviewer uuid.UUID, approve bool, reason string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	status := "rejected"
	if approve {
		status = "approved"
	}

	var placeID, action string
	var name, kind, target *string
	var lon, lat *float64
	err = tx.QueryRow(ctx, `
		UPDATE meeting_point_requests
		   SET status = $3, reviewed_by = $2, reviewed_at = now(), reason = nullif($4, '')
		 WHERE id = $1 AND status = 'pending'
		RETURNING place_id, action, target_id, name, kind, ST_X(point), ST_Y(point)`,
		id, reviewer, status, reason).
		Scan(&placeID, &action, &target, &name, &kind, &lon, &lat)
	if errors.Is(err, pgx.ErrNoRows) {
		// Either there is no such request, or somebody already decided it. The
		// caller cannot tell the two apart, and does not need to.
		return ErrAlreadyDecided
	}
	if err != nil {
		return fmt.Errorf("store: deciding the request: %w", err)
	}

	if approve {
		targetID := CuratedPrefix + id.String()
		if target != nil {
			targetID = *target
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO meeting_point_overrides
			    (target_id, place_id, action, name, kind, point, request_id)
			VALUES ($1, $2, $3, $4, $5,
			        CASE WHEN $6::float8 IS NULL THEN NULL
			             ELSE ST_SetSRID(ST_MakePoint($6, $7), 4326) END, $8)
			ON CONFLICT (target_id) DO UPDATE
			   SET action = EXCLUDED.action, name = EXCLUDED.name, kind = EXCLUDED.kind,
			       point = EXCLUDED.point, request_id = EXCLUDED.request_id,
			       updated_at = now()`,
			targetID, placeID, action, name, kind, lon, lat, id); err != nil {
			return fmt.Errorf("store: applying the decision: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// PendingMeetingPointRequests is the reviewer's queue, oldest first so nothing
// waits forever behind a busy day.
func PendingMeetingPointRequests(ctx context.Context, pool *pgxpool.Pool, limit int) ([]MeetingPointRequest, error) {
	return listRequests(ctx, pool, `
		SELECT id, place_id, action, target_id, name, kind, ST_X(point), ST_Y(point),
		       status, reason, created_at, reviewed_at
		  FROM meeting_point_requests
		 WHERE status = 'pending'
		 ORDER BY created_at
		 LIMIT $1`, limit)
}

// MeetingPointRequestsBy is somebody's own proposals and what became of them.
func MeetingPointRequestsBy(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, limit int) ([]MeetingPointRequest, error) {
	return listRequests(ctx, pool, `
		SELECT id, place_id, action, target_id, name, kind, ST_X(point), ST_Y(point),
		       status, reason, created_at, reviewed_at
		  FROM meeting_point_requests
		 WHERE proposed_by = $2
		 ORDER BY created_at DESC
		 LIMIT $1`, limit, userID)
}

func listRequests(ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) ([]MeetingPointRequest, error) {
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("store: listing meeting point requests: %w", err)
	}
	defer rows.Close()

	out := []MeetingPointRequest{}
	for rows.Next() {
		var r MeetingPointRequest
		if err := rows.Scan(&r.ID, &r.PlaceID, &r.Action, &r.TargetID, &r.Name, &r.Kind,
			&r.Lon, &r.Lat, &r.Status, &r.Reason, &r.CreatedAt, &r.Reviewed); err != nil {
			return nil, fmt.Errorf("store: scanning a meeting point request: %w", err)
		}
		// The settlement's name, so a reviewer is not reading OSM ids.
		if p, ok := places.ByID(r.PlaceID); ok {
			r.PlaceName = p.Name
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ModeratorEdit is a correction made directly: hiding a point that is not one,
// renaming one whose OSM label is a ticket agency, or moving a good one to the
// top of the list.
type ModeratorEdit struct {
	PlaceID  string
	Action   string // add, update or hide
	TargetID string // the point being changed; empty only when adding
	Name     string
	Kind     string
	Lon, Lat float64
	Priority int
	By       uuid.UUID
}

// ApplyModeratorEdit records the correction and applies it in one transaction.
//
// It goes through the same request table as anybody else's suggestion, already
// approved. A moderation system whose own actions leave no trace is one where
// the question "who removed this, and when" has no answer.
func ApplyModeratorEdit(ctx context.Context, pool *pgxpool.Pool, e ModeratorEdit) error {
	if e.Action != "add" && e.Action != "update" && e.Action != "hide" {
		return fmt.Errorf("store: %q is not something that can be done to a meeting point", e.Action)
	}
	if e.Action != "add" && e.TargetID == "" {
		return fmt.Errorf("store: a %s needs to say which point", e.Action)
	}
	place, ok := places.ByID(e.PlaceID)
	if !ok {
		return ErrUnknownPlace
	}

	var name *string
	var kind *string
	var lon, lat *float64
	if e.Action != "hide" {
		if e.Name != "" {
			normalised, err := domain.NormaliseName(e.Name)
			if err != nil {
				return err
			}
			name = &normalised
		}
		if e.Kind != "" {
			if !slicesContains(PointKinds, e.Kind) {
				return ErrBadKind
			}
			kind = &e.Kind
		}
		if e.Lon != 0 || e.Lat != 0 {
			if err := plausible(place, e.Lon, e.Lat); err != nil {
				return err
			}
			lon, lat = &e.Lon, &e.Lat
		}
		// Adding needs all three, which the schema also insists on; saying so
		// here gives a reason rather than a constraint violation.
		if e.Action == "add" && (name == nil || kind == nil || lon == nil) {
			return fmt.Errorf("store: a new point needs a name, a kind and a coordinate")
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var requestID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO meeting_point_requests
		    (place_id, action, target_id, name, kind, point, status,
		     proposed_by, reviewed_by, reviewed_at)
		VALUES ($1, $2, nullif($3, ''), $4, $5,
		        CASE WHEN $6::float8 IS NULL THEN NULL
		             ELSE ST_SetSRID(ST_MakePoint($6, $7), 4326) END,
		        'approved', $8, $8, now())
		RETURNING id`,
		e.PlaceID, e.Action, e.TargetID, name, kind, lon, lat, e.By).Scan(&requestID); err != nil {
		return fmt.Errorf("store: recording the correction: %w", err)
	}

	targetID := e.TargetID
	if targetID == "" {
		targetID = CuratedPrefix + requestID.String()
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO meeting_point_overrides
		    (target_id, place_id, action, name, kind, point, priority, request_id)
		VALUES ($1, $2, $3, $4, $5,
		        CASE WHEN $6::float8 IS NULL THEN NULL
		             ELSE ST_SetSRID(ST_MakePoint($6, $7), 4326) END, $8, $9)
		ON CONFLICT (target_id) DO UPDATE
		   SET action = EXCLUDED.action, name = EXCLUDED.name, kind = EXCLUDED.kind,
		       point = EXCLUDED.point, priority = EXCLUDED.priority,
		       request_id = EXCLUDED.request_id, updated_at = now()`,
		targetID, e.PlaceID, e.Action, name, kind, lon, lat, e.Priority, requestID); err != nil {
		return fmt.Errorf("store: applying the correction: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

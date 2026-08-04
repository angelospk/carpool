package store

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/places"
)

// MaxMeetingPoints is how many a settlement offers, however many exist.
//
// The harvest already capped itself at three for a city; this caps the merged
// list, so curating a place adds better points rather than more of them. A
// list nobody reads to the bottom of is a list with a wrong answer hiding in
// it.
const MaxMeetingPoints = 4

// CuratedPrefix marks a meeting point that was added here rather than
// harvested. It cannot collide with an OSM identity, which is a type and a
// number.
const CuratedPrefix = "curated/"

// override is one row of what differs from the harvested list.
type override struct {
	targetID string
	action   string
	name     string
	kind     string
	lon, lat float64
	priority int
}

// MeetingPointsFor returns what a settlement actually offers: the harvested
// landmarks with every approved correction applied.
//
// A failure to read the corrections is an error rather than a quiet fallback to
// the harvested list. Falling back would put a point somebody removed for a
// reason back in front of people, at exactly the moment nobody is watching.
func MeetingPointsFor(ctx context.Context, pool *pgxpool.Pool, placeID string) ([]places.MeetingPoint, error) {
	rows, err := pool.Query(ctx, `
		SELECT target_id, action,
		       coalesce(name, ''), coalesce(kind, ''),
		       coalesce(ST_X(point), 0), coalesce(ST_Y(point), 0),
		       priority
		  FROM meeting_point_overrides
		 WHERE place_id = $1
		 ORDER BY priority, target_id`, placeID)
	if err != nil {
		return nil, fmt.Errorf("store: reading meeting point overrides: %w", err)
	}
	defer rows.Close()

	edits := map[string]override{}
	var added []override
	for rows.Next() {
		var o override
		if err := rows.Scan(&o.targetID, &o.action, &o.name, &o.kind, &o.lon, &o.lat, &o.priority); err != nil {
			return nil, fmt.Errorf("store: scanning a meeting point override: %w", err)
		}
		edits[o.targetID] = o
		if o.action == "add" {
			added = append(added, o)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: reading meeting point overrides: %w", err)
	}

	out := make([]places.MeetingPoint, 0, MaxMeetingPoints)
	for _, p := range places.PointsFor(placeID) {
		o, edited := edits[p.ID]
		switch {
		case edited && o.action == "hide":
			continue
		case edited && o.action == "update":
			// The identity survives a correction: renaming "Τ.Σ. Ν.Σ. ΣΤΑΘΜΟΣ"
			// fixes a label rather than replacing a place, and anything already
			// pointing at it has to still find it. Only what was named in the
			// correction moves.
			if o.name != "" {
				p.Name = o.name
			}
			if o.kind != "" {
				p.Kind = o.kind
			}
			if o.lon != 0 || o.lat != 0 {
				p.Lon, p.Lat = o.lon, o.lat
			}
		}
		out = append(out, p)
	}

	// A curated point that was hidden later is not in this list at all: target_id
	// is the primary key, so changing its action to 'hide' is the same row
	// changing, not a second row overruling the first. That is the case a single
	// table could not express, and the reason there are two.
	for _, o := range added {
		out = append(out, places.MeetingPoint{
			ID: o.targetID, PlaceID: placeID, Kind: o.kind,
			Name: o.name, Lon: o.lon, Lat: o.lat,
		})
	}

	// Curated points can be put above the automatic ones, which is the whole
	// reason a moderator would add the ΚΤΕΛ to a city that already has three
	// poor points. Harvested points sit at priority zero and keep their
	// harvested order among themselves, so the sort has to be stable.
	slices.SortStableFunc(out, func(a, b places.MeetingPoint) int {
		return priorityOf(edits, a) - priorityOf(edits, b)
	})

	if len(out) > MaxMeetingPoints {
		out = out[:MaxMeetingPoints]
	}
	return out, nil
}

func priorityOf(edits map[string]override, p places.MeetingPoint) int {
	if o, ok := edits[p.ID]; ok {
		return o.priority
	}
	return 0
}

// SetModerator grants or revokes moderation, and records that it happened.
//
// The flag lives on the user and is read on every moderated request rather than
// carried in a token: a revoked moderator has to stop being one now, not when
// their access token expires. actor is uuid.Nil when the change came from the
// operator command, which is how the first moderator has to be made.
func SetModerator(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, granted bool, actor uuid.UUID, note string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `UPDATE users SET is_moderator = $2 WHERE id = $1`, userID, granted)
	if err != nil {
		return fmt.Errorf("store: setting the moderator flag: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("store: no user %s", userID)
	}

	var actorID *uuid.UUID
	if actor != uuid.Nil {
		actorID = &actor
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO moderator_grants (user_id, granted, actor_id, note)
		VALUES ($1, $2, $3, nullif($4, ''))`, userID, granted, actorID, note); err != nil {
		return fmt.Errorf("store: recording the grant: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

package match_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/match"
)

// A route a driver offered to their group reaches the group and nobody else.
//
// The restriction is the whole reason somebody would publish into a group
// rather than to everyone: "only people from the hospital". A matcher that let
// a stranger through would make the choice a label rather than a promise, and
// nobody would find out until the stranger was in the car.
func TestARouteOfferedToAGroupReachesOnlyItsLiveMembers(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	occurrence := f.addOccurrence(t, corridorWKT)

	var groupID uuid.UUID
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO groups (name, join_code) VALUES ('Νοσοκομείο', 'ACDEFGHJ') RETURNING id`).Scan(&groupID); err != nil {
		t.Fatalf("seeding group: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		UPDATE route_templates SET group_id = $1
		WHERE id = (SELECT template_id FROM trip_occurrences WHERE id = $2)`, groupID, occurrence); err != nil {
		t.Fatalf("restricting the route: %v", err)
	}

	member := f.passenger(t, "+306900000101")
	outsider := f.passenger(t, "+306900000102")
	former := f.passenger(t, "+306900000103")
	joined := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO group_members (group_id, user_id, role, joined_at, left_at)
		VALUES ($1, $2, 'member', $4, NULL), ($1, $3, 'member', $4, $4 + interval '1 day')`,
		groupID, member, former, joined); err != nil {
		t.Fatalf("seeding members: %v", err)
	}

	for _, tc := range []struct {
		name      string
		passenger uuid.UUID
		want      int
	}{
		{"a live member", member, 1},
		{"somebody outside the group", outsider, 0},
		{"somebody who has left", former, 0},
		{"nobody at all", uuid.Nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := query()
			q.PassengerID = tc.passenger
			got, err := match.Gross(ctx, f.pool, q)
			if err != nil {
				t.Fatalf("Gross: %v", err)
			}
			if len(got) != tc.want {
				t.Fatalf("got %d candidates, want %d", len(got), tc.want)
			}
		})
	}
}

// An unrestricted route is untouched by all of this: the column is null for
// every route published before groups existed, and they must keep matching.
func TestARouteWithoutAGroupStillReachesEverybody(t *testing.T) {
	f := setup(t)
	f.addOccurrence(t, corridorWKT)

	q := query()
	q.PassengerID = f.passenger(t, "+306900000104")
	got, err := match.Gross(context.Background(), f.pool, q)
	if err != nil {
		t.Fatalf("Gross: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1", len(got))
	}
}

func (f *fixture) passenger(t *testing.T, phone string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO users (date_of_birth, phone, name, home_region_id)
		VALUES (DATE '1990-01-01', $1, 'Passenger', $2) RETURNING id`, phone, f.regionID).Scan(&id); err != nil {
		t.Fatalf("seeding passenger: %v", err)
	}
	return id
}

package groups

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// VerifiedTrip is one confirmed ride between two members, as an organisation
// may see it: enough to pay somebody for it, and nothing else.
//
// No phone numbers, no coordinates, no price. The labels are the route's own
// town names, the names are the ones members already show each other, and the
// organisation holds both people's identities already — that is what it means
// for them to be in its group.
type VerifiedTrip struct {
	ServiceDate      time.Time
	DepartEarliest   time.Time
	OriginLabel      string
	DestinationLabel string
	RouteKm          float64
	Driver           string
	Passenger        string
	Seats            int
}

// Export lists a month of verified trips between members of the group, for one
// of its admins.
//
// A trip counts when three things hold:
//
//   - both people confirmed it happened. A status alone is not proof: it can be
//     reached by one side's word, and a subsidy paid on one side's word is the
//     easiest thing in the app to invent.
//   - the driver and the passenger were BOTH members on the day it ran.
//     Membership is a history for exactly this: somebody who joined last week is
//     not shown to have been a colleague in March, and somebody who left is not
//     shown afterwards. One member is not enough either — the export would then
//     tell an employer where their employee went with somebody who never agreed
//     to be counted by it.
//   - the service date falls in the month, read as an Athens calendar date,
//     which is what service_date already is.
//
// The month is YYYY-MM; the caller has already parsed it into its first day.
func (s *Service) Export(ctx context.Context, userID, groupID uuid.UUID, month time.Time) ([]VerifiedTrip, error) {
	role, err := s.roleOf(ctx, s.pool, userID, groupID)
	if err != nil {
		return nil, err
	}
	if role != "admin" {
		return nil, ErrNotAdmin
	}

	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	next := first.AddDate(0, 1, 0)

	rows, err := s.pool.Query(ctx, `
		SELECT o.service_date, o.depart_earliest, t.origin_label, t.destination_label, o.route_km,
		       du.name, pu.name, b.seats
		FROM bookings b
		JOIN trip_occurrences o ON o.id = b.occurrence_id
		JOIN route_templates t  ON t.id = o.template_id
		JOIN users du ON du.id = t.driver_id
		JOIN users pu ON pu.id = b.passenger_id
		WHERE b.driver_confirmed AND b.passenger_confirmed
		  AND b.status IN ('confirmed', 'completed')
		  AND o.service_date >= $2::date AND o.service_date < $3::date
		  AND EXISTS (SELECT 1 FROM group_members m
		              WHERE m.group_id = $1 AND m.user_id = t.driver_id
		                AND m.joined_at <= o.depart_earliest
		                AND (m.left_at IS NULL OR m.left_at > o.depart_earliest))
		  AND EXISTS (SELECT 1 FROM group_members m
		              WHERE m.group_id = $1 AND m.user_id = b.passenger_id
		                AND m.joined_at <= o.depart_earliest
		                AND (m.left_at IS NULL OR m.left_at > o.depart_earliest))
		ORDER BY o.depart_earliest, b.id`, groupID, first, next)
	if err != nil {
		return nil, fmt.Errorf("groups: exporting: %w", err)
	}
	defer rows.Close()

	out := []VerifiedTrip{}
	for rows.Next() {
		var v VerifiedTrip
		if err := rows.Scan(&v.ServiceDate, &v.DepartEarliest, &v.OriginLabel, &v.DestinationLabel,
			&v.RouteKm, &v.Driver, &v.Passenger, &v.Seats); err != nil {
			return nil, fmt.Errorf("groups: reading the export: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Stats is a month of a group, counted: how many are in it, how many of them
// offer a route, and the verified trips between members that the export would
// list. Numbers only; the export is where the names are.
type Stats struct {
	Members int
	Drivers int
	Trips   int
	Seats   int
}

// MonthStats counts the month containing now, for an admin, by the same rules
// as Export: both sides confirmed, both members on the day.
func (s *Service) MonthStats(ctx context.Context, userID, groupID uuid.UUID, now time.Time) (Stats, error) {
	role, err := s.roleOf(ctx, s.pool, userID, groupID)
	if err != nil {
		return Stats{}, err
	}
	if role != "admin" {
		return Stats{}, ErrNotAdmin
	}

	athens, err := time.LoadLocation("Europe/Athens")
	if err != nil {
		return Stats{}, fmt.Errorf("groups: time zone: %w", err)
	}
	local := now.In(athens)
	first := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, time.UTC)
	next := first.AddDate(0, 1, 0)

	var st Stats
	err = s.pool.QueryRow(ctx, `
		WITH live AS (
			SELECT user_id FROM group_members WHERE group_id = $1 AND left_at IS NULL
		), verified AS (
			SELECT b.seats
			FROM bookings b
			JOIN trip_occurrences o ON o.id = b.occurrence_id
			JOIN route_templates t  ON t.id = o.template_id
			WHERE b.driver_confirmed AND b.passenger_confirmed
			  AND b.status IN ('confirmed', 'completed')
			  AND o.service_date >= $2::date AND o.service_date < $3::date
			  AND EXISTS (SELECT 1 FROM group_members m
			              WHERE m.group_id = $1 AND m.user_id = t.driver_id
			                AND m.joined_at <= o.depart_earliest
			                AND (m.left_at IS NULL OR m.left_at > o.depart_earliest))
			  AND EXISTS (SELECT 1 FROM group_members m
			              WHERE m.group_id = $1 AND m.user_id = b.passenger_id
			                AND m.joined_at <= o.depart_earliest
			                AND (m.left_at IS NULL OR m.left_at > o.depart_earliest))
		)
		SELECT (SELECT count(*) FROM live),
		       (SELECT count(DISTINCT t.driver_id) FROM route_templates t
		        WHERE t.active AND t.driver_id IN (SELECT user_id FROM live)),
		       (SELECT count(*) FROM verified),
		       (SELECT coalesce(sum(seats), 0) FROM verified)`,
		groupID, first, next).Scan(&st.Members, &st.Drivers, &st.Trips, &st.Seats)
	if err != nil {
		return Stats{}, fmt.Errorf("groups: counting the month: %w", err)
	}
	return st, nil
}

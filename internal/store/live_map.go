package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LiveMapTrip is one car on the road now, as anybody signed in may see it:
// where it goes, as settlement names and a line, and how many seats are free.
// No driver, no passengers, no live position. The line stops a kilometre short
// of each end, because where a route starts is often somebody's front door.
//
// From and To are left for the caller to fill from the ends, which never leave
// the server: the driver's own labels can be a street or a typed coordinate.
type LiveMapTrip struct {
	From      string `json:"from"`
	To        string `json:"to"`
	SeatsFree int    `json:"seats_free"`
	Polyline  string `json:"polyline"`

	OriginLon, OriginLat, DestinationLon, DestinationLat float64 `json:"-"`
}

// liveMapLimit caps the answer. Evros on a busy morning is a few dozen cars;
// a map with more lines than this is unreadable anyway.
const liveMapLimit = 200

// LiveMap lists the public trips underway at now: departed and not yet due to
// have arrived. A route offered to one group stays that group's business.
func LiveMap(ctx context.Context, pool *pgxpool.Pool, now time.Time) ([]LiveMapTrip, error) {
	rows, err := pool.Query(ctx, `
		SELECT o.seats_free,
		       ST_AsEncodedPolyline(
		         ST_Simplify(
		           ST_LineSubstring(o.route_geom,
		             least(0.45, 1000 / ST_Length(o.route_geom::geography)),
		             greatest(0.55, 1 - 1000 / ST_Length(o.route_geom::geography))),
		           0.0005),
		         6),
		       ST_X(t.origin), ST_Y(t.origin), ST_X(t.destination), ST_Y(t.destination)
		  FROM trip_occurrences o
		  JOIN route_templates t ON t.id = o.template_id
		 WHERE o.status IN ('scheduled', 'started')
		   AND t.group_id IS NULL
		   AND ST_Length(o.route_geom::geography) > 2500
		   AND o.depart_earliest <= $1
		   AND o.depart_latest + make_interval(mins => o.route_min) >= $1
		 ORDER BY o.depart_earliest
		 LIMIT $2`, now, liveMapLimit)
	if err != nil {
		return nil, fmt.Errorf("store: live map: %w", err)
	}
	defer rows.Close()

	out := []LiveMapTrip{}
	for rows.Next() {
		var t LiveMapTrip
		if err := rows.Scan(&t.SeatsFree, &t.Polyline,
			&t.OriginLon, &t.OriginLat, &t.DestinationLon, &t.DestinationLat); err != nil {
			return nil, fmt.Errorf("store: live map: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

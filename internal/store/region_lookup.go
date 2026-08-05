package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrOutsideGreece means a coordinate belongs to no region and is too far from
// one to be a rounding error.
//
// It is deliberately not ErrNotFound. "This town is in Ροδόπη" and "this point
// is in Bulgaria" are different answers, and a caller that treats the second as
// the first hands somebody a Greek fuel price for a country the app does not
// serve.
var ErrOutsideGreece = errors.New("store: that point is outside Greece")

// nearestToleranceMetres is how far off a boundary a point may be and still be
// resolved to it.
//
// The 75 regions tile the country and its territorial waters completely — a
// berth at Ραφήνα, a point in the middle of the Aegean and Άγιο Όρος are all
// covered — so on today's fixture this never fires. It is here for the seam
// that simplification can open: rings are simplified one at a time, so two
// neighbours no longer meet along exactly the same line, and a re-harvest could
// leave a village of eighty people in a gap a couple of hundred metres wide.
//
// Two kilometres is an order of magnitude more than such a seam and nowhere
// near enough to reach another country. Being wrong inside it costs the
// difference between two adjacent fuel prices; refusing costs a pickup the app
// cannot price at all.
const nearestToleranceMetres = 2000

// RegionAt names the περιφερειακή ενότητα a coordinate falls in.
//
// ST_Covers rather than ST_Contains: a point exactly on the boundary line is
// in the region, and coordinates that came from a boundary — a town on a river
// that is also the border — land on it more often than chance suggests.
func RegionAt(ctx context.Context, pool *pgxpool.Pool, lon, lat float64) (Region, error) {
	var r Region
	err := pool.QueryRow(ctx, `
		WITH p AS (SELECT ST_SetSRID(ST_MakePoint($1, $2), 4326) AS geom)
		SELECT id, name
		FROM regions, p
		WHERE ST_Covers(boundary, p.geom)
		-- A point on a shared line is covered by both neighbours. Ordering by
		-- name rather than leaving it to the planner means the same point
		-- resolves to the same region every time, which matters because the
		-- answer is frozen into a booking's receipt.
		ORDER BY name
		LIMIT 1`, lon, lat).Scan(&r.ID, &r.Name)
	if err == nil {
		return r, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Region{}, fmt.Errorf("store: locating region: %w", err)
	}

	// Nothing covers it. Either it is in a sliver between two simplified
	// boundaries, or in the sea beside one, or in another country.
	err = pool.QueryRow(ctx, `
		WITH p AS (SELECT ST_SetSRID(ST_MakePoint($1, $2), 4326) AS geom)
		SELECT id, name
		FROM regions, p
		WHERE ST_DWithin(boundary::geography, p.geom::geography, $3)
		-- <-> is the GiST distance operator, so this is an index scan rather
		-- than 74 polygon comparisons.
		ORDER BY boundary <-> p.geom, name
		LIMIT 1`, lon, lat, nearestToleranceMetres).Scan(&r.ID, &r.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return Region{}, ErrOutsideGreece
	}
	if err != nil {
		return Region{}, fmt.Errorf("store: locating nearest region: %w", err)
	}
	return r, nil
}

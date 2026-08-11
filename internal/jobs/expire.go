package jobs

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
)

// RequestSweeper finishes ride requests whose window has passed.
//
// Two places already assumed this existed. match.Waiting reads the window
// rather than the status and explains that a request is "only marked expired
// when the sweeper next runs"; the reconciler says the same. Nothing did. A
// request stayed 'open' from the day it was made until the retention janitor
// removed it two years later, and 'expired' was a status the schema allowed and
// no code path could produce.
//
// Neither filter would ever match one, so what this fixes is not somebody being
// offered a lift to a journey they have already made. It is that the column
// meant nothing: the data export showed a journey from last spring still
// looking for a driver, and anything built later that lists somebody's requests
// would have had to rediscover the window rule rather than read the status.
type RequestSweeper struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

func NewRequestSweeper(pool *pgxpool.Pool, c clock.Clock) *RequestSweeper {
	return &RequestSweeper{pool: pool, clock: c}
}

// Run marks the passed-over requests expired and returns how many.
//
// Only 'open' ones. A request being put to a driver is left where it is: that
// offer lives half an hour at most and ends by putting the request back to
// 'open', which is where the next pass finds it. Sweeping it out from under a
// live offer would leave a driver holding one for a request the system had
// already called finished.
func (s *RequestSweeper) Run(ctx context.Context) (int, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE ride_requests SET status = 'expired', updated_at = now()
		 WHERE status = 'open' AND window_end <= $1`, s.clock.Now())
	if err != nil {
		return 0, fmt.Errorf("jobs: expiring passed-over requests: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

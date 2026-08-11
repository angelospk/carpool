-- +goose Up

-- Indexes for the reverse matching direction.
--
-- ride_requests already has GIST indexes on origin and destination, but they
-- are on the plain geometry and the proximity test casts to geography for
-- metre-accurate distances. A cast is a different expression, so those indexes
-- could not serve it and the query scanned every open request. Same reason
-- route_templates carries an expression index on the cast rather than on the
-- column; this is the mirror of it.
--
-- Measured on 5,000 open requests: 11.5 ms to 5.6 ms, and the plan changes from
-- a sequential scan discarding 4,984 rows to an index scan discarding none.
-- Halving the time is not the point. The sequential scan grows with every open
-- request in the system and the index scan does not, so the number to look at
-- is the one that was going to be a problem at 50,000 rather than the one that
-- was already fine at 5,000.
--
-- Partial on status = 'open' because that is the only status the reverse query
-- looks at, and it is a small live set inside a table that accumulates settled
-- and expired rows for two years. The index stays the size of the traffic
-- rather than the size of the history.
CREATE INDEX ride_requests_origin_geog ON ride_requests
    USING GIST ((origin::geography)) WHERE status = 'open';
CREATE INDEX ride_requests_dest_geog ON ride_requests
    USING GIST ((destination::geography)) WHERE status = 'open';

-- +goose Down

DROP INDEX IF EXISTS ride_requests_dest_geog;
DROP INDEX IF EXISTS ride_requests_origin_geog;

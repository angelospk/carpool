-- +goose Up

-- Where somebody is, right now, during a trip they are on. One row per person
-- per trip, overwritten on every update and deleted when they stop sharing or
-- within the hour after. Not a history: nothing here answers "where was she at
-- 07:40 last Tuesday", and that is the point of it having one row.
--
-- Plain columns rather than a geometry: nothing queries these spatially, and
-- a point that is read, shown and thrown away does not need an index to find it.
CREATE TABLE trip_live_positions (
    occurrence_id uuid NOT NULL REFERENCES trip_occurrences(id) ON DELETE CASCADE,
    user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    lat           double precision NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lon           double precision NOT NULL CHECK (lon BETWEEN -180 AND 180),
    updated_at    timestamptz NOT NULL,
    PRIMARY KEY (occurrence_id, user_id)
);
CREATE INDEX trip_live_positions_updated ON trip_live_positions (updated_at);

-- +goose Down
DROP TABLE trip_live_positions;

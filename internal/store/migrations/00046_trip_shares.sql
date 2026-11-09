-- +goose Up
-- A passenger's link to their trip, for the people who should know whose car
-- they got into. Whoever holds the link sees what the passenger sees of the
-- driver while the trip is on: the car, and where it is.
--
-- One link per passenger and trip. Revoking clears it; sharing again makes a
-- new one, so a link given to the wrong person can be taken back.

CREATE TABLE trip_shares (
    token          text PRIMARY KEY,
    occurrence_id  uuid NOT NULL REFERENCES trip_occurrences (id) ON DELETE CASCADE,
    passenger_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (occurrence_id, passenger_id)
);

-- +goose Down
DROP TABLE trip_shares;

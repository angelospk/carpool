-- +goose Up
-- How long a drive goes on taking offers. A pinned departure has left at its
-- minute, but the car is still on the road with seats, and people further
-- along it can still be reached: the matcher keeps it open until this, and
-- holds every pickup to being ahead of the car in time.
ALTER TABLE trip_occurrences ADD COLUMN offers_until timestamptz NULL;

-- +goose Down
ALTER TABLE trip_occurrences DROP COLUMN offers_until;

-- +goose Up
-- How far off their road a driver will go for a passenger on this drive, in
-- minutes of driving and nothing else. The route's own detour_min is a total
-- that includes two minutes of pulling over per stop, so "0, my road only"
-- written there would refuse even somebody standing on the road. Null until
-- the driver chooses; then it replaces the time limit for this departure.
ALTER TABLE trip_occurrences
    ADD COLUMN drive_detour_min int NULL CHECK (drive_detour_min BETWEEN 0 AND 30);

-- +goose Down
ALTER TABLE trip_occurrences DROP COLUMN drive_detour_min;

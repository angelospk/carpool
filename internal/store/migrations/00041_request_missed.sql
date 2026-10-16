-- +goose Up
-- Why the matcher's last look found nobody to ask, when a car did pass: its
-- driver takes no stops (no_stops), or the detour is longer than the driver
-- takes, in minutes of driving (detour_min) or kilometres (detour_km). need
-- is what the passenger's pickup would take, limit what the driver agreed to.
-- The passenger is told this without a name: the driver agreed to nothing.
-- Cleared when a driver is asked.
ALTER TABLE ride_requests
    ADD COLUMN missed_reason text NULL CHECK (missed_reason IN ('no_stops', 'detour_min', 'detour_km')),
    ADD COLUMN missed_need   real NULL,
    ADD COLUMN missed_limit  real NULL;

-- +goose Down
ALTER TABLE ride_requests
    DROP COLUMN missed_limit,
    DROP COLUMN missed_need,
    DROP COLUMN missed_reason;

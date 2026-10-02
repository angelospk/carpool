-- +goose Up
-- What a moving drive needs to know about the car: how good the last fix was
-- and when the phone took it. The server's own clock says when it arrived,
-- which on a phone that batched three fixes in a tunnel is not when the car
-- was there.
ALTER TABLE trip_live_positions
    ADD COLUMN accuracy_m  real NULL CHECK (accuracy_m >= 0),
    ADD COLUMN observed_at timestamptz NULL;

-- The driver said «Τέλος». After it nothing the driver's phone sends about
-- this trip is written: a request still in flight would otherwise put the
-- position back.
ALTER TABLE trip_occurrences ADD COLUMN drive_ended_at timestamptz NULL;

-- «Είμαι εδώ»: the passenger is at the pickup.
ALTER TABLE bookings ADD COLUMN arrived_at timestamptz NULL;

-- +goose Down
ALTER TABLE bookings DROP COLUMN arrived_at;
ALTER TABLE trip_occurrences DROP COLUMN drive_ended_at;
ALTER TABLE trip_live_positions DROP COLUMN observed_at, DROP COLUMN accuracy_m;

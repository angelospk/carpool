-- +goose Up
-- A trip that runs into a ferry stops at the port.
--
-- OSRM will route a car from Athens to Mykonos and answer 221 km, because the
-- car profile treats a ferry as a very slow road. Two things follow from that
-- and both are wrong: the duration is fiction (Kos to Rhodes came back as
-- twenty hours), and the price is worse, because the fuel formula divides
-- petrol over distance and would charge a driver for a hundred and fifty
-- kilometres of sea. The ticket for the car is not in the formula at all.
--
-- So the route is cut at the water and the trip is the driving. This column is
-- what lets the app say so, rather than quietly showing a short trip to a
-- destination the driver never named.

ALTER TABLE route_templates ADD COLUMN ends_at_port boolean NOT NULL DEFAULT false;

-- Snapshotted onto the occurrence with the rest of the template, so that a
-- trip somebody already booked keeps saying what it said when they booked it.
ALTER TABLE trip_occurrences ADD COLUMN ends_at_port boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE route_templates DROP COLUMN ends_at_port;
ALTER TABLE trip_occurrences DROP COLUMN ends_at_port;

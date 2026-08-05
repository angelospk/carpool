-- +goose Up

-- How many, not just how big.
--
-- 00023 gave a route the largest thing it would carry and a request the largest
-- thing somebody was bringing. That answers "will the bicycle fit" and says
-- nothing about "will four suitcases fit", which is the question three
-- passengers with a suitcase each are actually asking. GoRide's own booking
-- request captures "τον αριθμό και το μέγεθος των αποσκευών"; ours captured only
-- the second half.
--
-- The model is uniform slots, on Codex's advice and after rejecting a volume
-- score. A driver offers N places for an item of at most the stated size:
--
--   three cabin-sized places do not take a suitcase
--   three suitcase-sized places do take three cabin bags
--   somebody with one suitcase and two small bags declares three suitcase-sized
--   items, which under-uses the boot but cannot over-promise it
--
-- A score would have to claim that three cabin bags equal one suitcase, which is
-- false in a way that depends on the shape of the boot and on nothing the app
-- can see.

-- What the boot holds, in items of the size the route already states.
--
-- Three by default: an ordinary boot on an ordinary trip with three seats on
-- offer. Zero is only allowed together with 'none', because "I will take nothing"
-- and "I will take four of nothing" are the same sentence and only one of them
-- should be storable.
ALTER TABLE route_templates
    ADD COLUMN luggage_capacity smallint NOT NULL DEFAULT 3;

UPDATE route_templates SET luggage_capacity = 0 WHERE luggage = 'none';

ALTER TABLE route_templates ADD CONSTRAINT route_templates_luggage_capacity_sane CHECK (
    (luggage =  'none' AND luggage_capacity = 0) OR
    (luggage <> 'none' AND luggage_capacity BETWEEN 1 AND 8)
);

-- Snapshotted onto the departure like everything else the matcher reads, so
-- that editing a route does not change what a booked trip agreed to carry.
ALTER TABLE trip_occurrences
    ADD COLUMN luggage_capacity smallint NOT NULL DEFAULT 3;

UPDATE trip_occurrences o SET luggage_capacity = t.luggage_capacity
  FROM route_templates t WHERE t.id = o.template_id;

ALTER TABLE trip_occurrences ADD CONSTRAINT trip_occurrences_luggage_capacity_sane CHECK (
    (luggage =  'none' AND luggage_capacity = 0) OR
    (luggage <> 'none' AND luggage_capacity BETWEEN 1 AND 8)
);

-- And how many the passenger is bringing. Four is the ceiling: somebody with
-- five items is moving house, and there is a conversation to have rather than a
-- form to fill in.
--
-- The default is zero and not one, because it has to agree with the default on
-- the column beside it. 00023 made an unstated luggage size 'none', so an
-- unstated count of one would be "one item of nothing" — a row that contradicts
-- itself and that the constraint below would refuse. A default that cannot
-- coexist with its neighbour's default is a default that only works while
-- nobody uses it.
ALTER TABLE ride_requests
    ADD COLUMN luggage_count smallint NOT NULL DEFAULT 0;

ALTER TABLE ride_requests ADD CONSTRAINT ride_requests_luggage_count_sane CHECK (
    (luggage =  'none' AND luggage_count = 0) OR
    (luggage <> 'none' AND luggage_count BETWEEN 1 AND 4)
);

-- Copied onto the booking, which is what makes the space actually accounted for.
--
-- Deliberately NOT a running counter on the occurrence, the way seats_free is.
-- Seats are given back in six different places — cancel by passenger, cancel by
-- driver, erasure, suspension, the standing-arrangement path and the janitor —
-- and a seventh counter threaded through all of them is a capacity leak waiting
-- for the one that gets missed. Space taken is derived from the bookings that
-- hold it, so every one of those paths releases it by doing what it already
-- does. The occurrence row is locked FOR UPDATE while the sum is taken, which is
-- what makes it safe against two people booking the last place at once.
ALTER TABLE bookings
    ADD COLUMN luggage_count smallint NOT NULL DEFAULT 0
        CHECK (luggage_count BETWEEN 0 AND 4);

UPDATE bookings b SET luggage_count = r.luggage_count
  FROM ride_requests r WHERE r.id = b.request_id;

-- The sum is taken per occurrence over live bookings, so that is what it is
-- indexed by. 'confirmed' and 'completed' are the statuses that hold space; a
-- cancelled booking holds none, which is the whole reason this is derived.
CREATE INDEX bookings_occurrence_luggage_idx
    ON bookings (occurrence_id)
    WHERE status IN ('confirmed', 'completed');

-- +goose Down

DROP INDEX IF EXISTS bookings_occurrence_luggage_idx;
ALTER TABLE bookings         DROP COLUMN IF EXISTS luggage_count;
ALTER TABLE ride_requests    DROP CONSTRAINT IF EXISTS ride_requests_luggage_count_sane;
ALTER TABLE ride_requests    DROP COLUMN IF EXISTS luggage_count;
ALTER TABLE trip_occurrences DROP CONSTRAINT IF EXISTS trip_occurrences_luggage_capacity_sane;
ALTER TABLE trip_occurrences DROP COLUMN IF EXISTS luggage_capacity;
ALTER TABLE route_templates  DROP CONSTRAINT IF EXISTS route_templates_luggage_capacity_sane;
ALTER TABLE route_templates  DROP COLUMN IF EXISTS luggage_capacity;

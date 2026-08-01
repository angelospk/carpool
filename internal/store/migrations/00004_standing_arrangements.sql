-- +goose Up

-- A standing arrangement is the record of a deal two people already made: this
-- passenger rides this driver's route every Tuesday until a date. The driver
-- approves once; after that the system stops asking, and stops handing the seat
-- to someone else.
--
-- The weekday is the Athens calendar weekday, taken from trip_occurrences'
-- service_date rather than from a departure instant: DST moves the instant, not
-- the day people mean.
CREATE TABLE standing_arrangements (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id       uuid NOT NULL REFERENCES route_templates(id) ON DELETE RESTRICT,
    passenger_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    weekday           int  NOT NULL CHECK (weekday BETWEEN 1 AND 7), -- ISO: Monday is 1
    seats             int  NOT NULL CHECK (seats BETWEEN 1 AND 4),
    pickup_point      geometry(Point, 4326) NOT NULL,
    dropoff_point     geometry(Point, 4326) NOT NULL,

    -- Frozen at creation and copied onto every booking it makes. Recalculating
    -- the contribution week by week would change a term of the agreement that
    -- neither side revisited.
    price_eur         numeric(5,2) NOT NULL CHECK (price_eur >= 0),

    -- The booking this was repeated from, for audit and for the idempotency of
    -- a retried creation.
    source_booking_id uuid REFERENCES bookings(id) ON DELETE SET NULL,

    status            text NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending','active','declined','cancelled','expired')),
    until_date        date NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT standing_arrangements_srid CHECK (
        ST_SRID(pickup_point) = 4326 AND ST_SRID(dropoff_point) = 4326
    )
);

-- A second switch for the same Tuesday is the same agreement, not another one.
-- Terminal rows are excluded so a cancelled arrangement does not block a fresh
-- one, and so an expiry sweep frees the slot.
CREATE UNIQUE INDEX standing_one_live_per_pair
    ON standing_arrangements (template_id, passenger_id, weekday)
    WHERE status IN ('pending','active');

CREATE INDEX standing_arrangements_live
    ON standing_arrangements (template_id, weekday, until_date)
    WHERE status = 'active';

CREATE INDEX standing_arrangements_passenger ON standing_arrangements (passenger_id);

-- ------------------------------------------------------------- bookings ---

-- A booking now arrives one of two ways: a driver accepted an ask, or a driver
-- approved a standing arrangement once and the weeks followed. The second kind
-- has no ask and no request — inventing a weekly request to preserve the old
-- shape would put decisions in the passenger's history that they never made.
ALTER TABLE bookings ADD COLUMN arrangement_id uuid
    REFERENCES standing_arrangements(id) ON DELETE RESTRICT;

ALTER TABLE bookings ALTER COLUMN ask_id     DROP NOT NULL;
ALTER TABLE bookings ALTER COLUMN request_id DROP NOT NULL;

-- Exactly one provenance, never both and never neither. Stated in full rather
-- than as "one of them is null", so a booking that carries a stray request
-- alongside an arrangement is refused too.
ALTER TABLE bookings ADD CONSTRAINT bookings_one_provenance CHECK (
    (arrangement_id IS     NULL AND ask_id IS NOT NULL AND request_id IS NOT NULL) OR
    (arrangement_id IS NOT NULL AND ask_id IS     NULL AND request_id IS     NULL)
);

-- What keeps a cancelled week cancelled is the UNIQUE (occurrence_id,
-- passenger_id) that bookings already carries: it counts cancelled rows too, so
-- a pass looking for "weeks this arrangement has not booked yet" cannot recreate
-- the day the passenger called off. No second index is added to say it twice.
CREATE INDEX bookings_arrangement ON bookings (arrangement_id)
    WHERE arrangement_id IS NOT NULL;

-- +goose Down

-- Irreversible, and it says so rather than failing halfway through. Restoring
-- NOT NULL on ask_id demands that no booking came from an arrangement, so a
-- working down migration would have to delete real bookings — seats people are
-- holding — to make the schema fit. That is a decision for a person, not for a
-- rollback.
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'migration 00004 is irreversible: rolling it back would mean deleting the bookings that came from standing arrangements';
END $$;
-- +goose StatementEnd

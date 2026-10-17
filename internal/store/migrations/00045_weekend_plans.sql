-- +goose Up
-- «Απόδραση το Σαββατοκύριακο»: a passenger who leaves town most weekends says
-- so once, and the server turns that into requests each week (specs/WEEKEND.md).
--
-- A plan is not a request. It never matches anybody itself; an hourly job makes
-- the requests of the coming weekend from it, and those carry the plan's id so
-- the matcher can leave them alone: the driver says yes first, from the publish
-- screen, and only then is the passenger told.

CREATE TABLE weekend_plans (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    passenger_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    origin            geometry(Point, 4326) NOT NULL,
    destination       geometry(Point, 4326) NOT NULL,
    origin_label      text,
    destination_label text,

    -- Athens wall time. 0 is Sunday, as in Go's time.Weekday and Postgres's
    -- EXTRACT(dow ...). The service only accepts 5, 6 and 0: it is a weekend.
    depart_dow        smallint NOT NULL CHECK (depart_dow BETWEEN 0 AND 6),
    depart_time       time NOT NULL,
    -- Both or neither: a return day with no time, or a time with no day, is
    -- not a return.
    return_dow        smallint CHECK (return_dow BETWEEN 0 AND 6),
    return_time       time,

    seats             smallint NOT NULL DEFAULT 1 CHECK (seats BETWEEN 1 AND 4),
    luggage           luggage_size NOT NULL DEFAULT 'none',
    luggage_count     smallint NOT NULL DEFAULT 0,

    every_week        boolean NOT NULL DEFAULT true,
    active            boolean NOT NULL DEFAULT true,
    -- «Όχι αυτή την εβδομάδα» answers in a row. At three the app offers to
    -- stop the plan; any «Ναι» sets it back to zero.
    declines_in_row   integer NOT NULL DEFAULT 0 CHECK (declines_in_row >= 0),
    -- The Friday of the weekend the Thursday question was last sent for, so
    -- the hourly job asks once per plan per weekend.
    last_asked_for    date,
    -- The Friday of the weekend the passenger last answered the Thursday
    -- question for, yes or no, so the app does not ask again after a restart.
    answered_for      date,

    -- The client's own key for the create, so a request sent again after a
    -- timeout is the plan it already made and not a second one. Optional: a
    -- client without one gets a plan every time.
    client_key        uuid,

    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT weekend_plans_client_key_once UNIQUE (passenger_id, client_key),
    CONSTRAINT weekend_plans_return_both CHECK ((return_dow IS NULL) = (return_time IS NULL)),
    CONSTRAINT weekend_plans_srid CHECK (ST_SRID(origin) = 4326 AND ST_SRID(destination) = 4326),
    CONSTRAINT weekend_plans_luggage_count_sane CHECK (
        (luggage =  'none' AND luggage_count = 0) OR
        (luggage <> 'none' AND luggage_count BETWEEN 1 AND 4)
    )
);

CREATE INDEX weekend_plans_by_passenger ON weekend_plans (passenger_id) WHERE active;

-- The requests a plan makes. SET NULL, not CASCADE: deleting a plan must not
-- delete a booking's request out from under it.
ALTER TABLE ride_requests
    ADD COLUMN weekend_plan_id uuid NULL REFERENCES weekend_plans (id) ON DELETE SET NULL,
    -- The Friday of the weekend the request is for, in Athens.
    ADD COLUMN weekend_of      date NULL,
    ADD COLUMN weekend_leg     text NULL CHECK (weekend_leg IN ('out', 'back'));

-- Generation is idempotent by this key: one request per plan, weekend, leg and
-- day. window_start tells the days of a leg apart (each day opens at its own
-- time). A second run of the job, or two API replicas running it together,
-- inserts nothing the first did not.
CREATE UNIQUE INDEX ride_requests_weekend_once
    ON ride_requests (weekend_plan_id, weekend_of, weekend_leg, window_start)
    WHERE weekend_plan_id IS NOT NULL;

-- Who made an ask: the matcher, asking a driver about a passenger, or a driver
-- offering a seat to a passenger who is waiting (specs/WEEKEND.md, the
-- driver's offer).
ALTER TABLE asks
    ADD COLUMN offered_by text NOT NULL DEFAULT 'matcher'
        CHECK (offered_by IN ('matcher', 'driver'));

-- +goose Down
ALTER TABLE asks DROP COLUMN offered_by;
DROP INDEX ride_requests_weekend_once;
ALTER TABLE ride_requests
    DROP COLUMN weekend_leg,
    DROP COLUMN weekend_of,
    DROP COLUMN weekend_plan_id;
DROP TABLE weekend_plans;

-- +goose Up

CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ---------------------------------------------------------------- regions ---

CREATE TABLE regions (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Unique so seeding can upsert by name and stay idempotent across deploys.
    name           text NOT NULL UNIQUE,
    boundary       geometry(MultiPolygon, 4326) NOT NULL,
    fuel_price_eur numeric(4,3) NOT NULL CHECK (fuel_price_eur > 0),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT regions_boundary_srid CHECK (ST_SRID(boundary) = 4326),
    CONSTRAINT regions_boundary_valid CHECK (ST_IsValid(boundary))
);

CREATE INDEX regions_boundary_gist ON regions USING GIST (boundary);

-- ------------------------------------------------------------------ users ---

CREATE TABLE users (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    phone          text NOT NULL UNIQUE, -- E.164, verified
    name           text NOT NULL,
    photo_url      text,
    gender         text CHECK (gender IN ('female','male','other')),
    home_region_id uuid NOT NULL REFERENCES regions(id),
    women_only     boolean NOT NULL DEFAULT false,
    status         text NOT NULL DEFAULT 'active'
                   CHECK (status IN ('active','suspended','deleted')),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT users_phone_e164 CHECK (phone ~ '^\+[1-9][0-9]{7,14}$')
);

CREATE TABLE driver_profiles (
    user_id                  uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    car_model                text NOT NULL,
    car_color                text,
    car_plate                text NOT NULL, -- revealed only post-accept
    fuel_consumption_l_100km numeric(4,1) NOT NULL DEFAULT 7.0
                             CHECK (fuel_consumption_l_100km > 0 AND fuel_consumption_l_100km < 100),
    wear_factor              numeric(3,2) NOT NULL DEFAULT 1.0
                             CHECK (wear_factor BETWEEN 1.0 AND 1.5),
    free_below_km            numeric(4,1) NOT NULL DEFAULT 5.0
                             CHECK (free_below_km BETWEEN 0 AND 20),
    notify_radius_km         numeric(4,1) NOT NULL DEFAULT 2.0
                             CHECK (notify_radius_km > 0 AND notify_radius_km <= 3.0),
    detour_budget_min        int NOT NULL DEFAULT 10
                             CHECK (detour_budget_min BETWEEN 0 AND 30),
    notifications_muted      boolean NOT NULL DEFAULT false,
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now()
);

-- ------------------------------------------------------------------- auth ---

-- OTP codes are stored hashed: a database leak must not hand out live logins.
CREATE TABLE otp_codes (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    phone        text NOT NULL,
    code_hash    text NOT NULL,
    expires_at   timestamptz NOT NULL,
    consumed_at  timestamptz,
    attempts     int NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT otp_codes_phone_e164 CHECK (phone ~ '^\+[1-9][0-9]{7,14}$')
);

CREATE INDEX otp_codes_phone_created ON otp_codes (phone, created_at DESC);
-- At most one live code per phone, so a resend invalidates its predecessor.
CREATE UNIQUE INDEX otp_codes_one_live ON otp_codes (phone) WHERE consumed_at IS NULL;

-- Refresh tokens are stored as digests and rotated on use, which is what makes
-- logout and replay detection possible.
CREATE TABLE refresh_tokens (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  text NOT NULL UNIQUE,
    family_id   uuid NOT NULL, -- shared by a rotation chain, revoked as a unit
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    revoked_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX refresh_tokens_user ON refresh_tokens (user_id);
CREATE INDEX refresh_tokens_family ON refresh_tokens (family_id);

CREATE TABLE push_tokens (
    token       text PRIMARY KEY, -- Expo push token
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    platform    text NOT NULL CHECK (platform IN ('ios','android')),
    disabled_at timestamptz, -- set on a DeviceNotRegistered receipt
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX push_tokens_user_live ON push_tokens (user_id) WHERE disabled_at IS NULL;

-- ----------------------------------------------------------------- routes ---

CREATE TABLE route_templates (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    driver_id         uuid NOT NULL REFERENCES driver_profiles(user_id) ON DELETE CASCADE,
    origin            geometry(Point, 4326) NOT NULL,
    destination       geometry(Point, 4326) NOT NULL,
    origin_label      text NOT NULL,
    destination_label text NOT NULL,
    route_geom        geometry(LineString, 4326) NOT NULL, -- OSRM-snapped
    route_km          numeric(6,1) NOT NULL CHECK (route_km > 0),
    route_min         int NOT NULL CHECK (route_min > 0),
    seats             int NOT NULL DEFAULT 3 CHECK (seats BETWEEN 1 AND 8),
    pricing_mode      text NOT NULL DEFAULT 'formula'
                      CHECK (pricing_mode IN ('formula','free_contribution','free')),
    osrm_data_version text NOT NULL,
    active            boolean NOT NULL DEFAULT true,
    revision          int NOT NULL DEFAULT 1, -- bumped on every edit
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT route_templates_srid CHECK (
        ST_SRID(origin) = 4326 AND ST_SRID(destination) = 4326 AND ST_SRID(route_geom) = 4326
    ),
    -- ST_LineLocatePoint cannot establish a unique traversal position on a
    -- self-intersecting line, and the gross match depends on it, so reject
    -- non-simple geometry at write time rather than mismatching later.
    CONSTRAINT route_templates_geom_simple CHECK (ST_IsSimple(route_geom)),
    CONSTRAINT route_templates_geom_length CHECK (ST_NPoints(route_geom) >= 2)
);

-- The gross match filters with ST_DWithin over route_geom::geography, so the
-- index must be on exactly that expression. See RISKS.md open decision 1: the
-- buffered functional index DATA-MODEL.md originally specced could never be
-- used by this predicate.
CREATE INDEX route_templates_geog_gist ON route_templates USING GIST ((route_geom::geography));
CREATE INDEX route_templates_driver ON route_templates (driver_id) WHERE active;

CREATE TABLE route_schedules (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id     uuid NOT NULL UNIQUE REFERENCES route_templates(id) ON DELETE CASCADE,
    kind            text NOT NULL CHECK (kind IN ('oneoff','recurring')),
    oneoff_date     date,
    weekdays        int[], -- ISO 1..7
    depart_earliest time NOT NULL, -- local Europe/Athens wall time
    depart_latest   time NOT NULL,
    effective_from  date NOT NULL DEFAULT current_date,
    effective_until date,
    exceptions      date[] NOT NULL DEFAULT '{}',
    revision        int NOT NULL DEFAULT 1,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT route_schedules_kind_fields CHECK (
        (kind = 'oneoff'    AND oneoff_date IS NOT NULL AND weekdays IS NULL) OR
        (kind = 'recurring' AND oneoff_date IS NULL     AND weekdays IS NOT NULL
                            AND array_length(weekdays, 1) BETWEEN 1 AND 7)
    ),
    CONSTRAINT route_schedules_window CHECK (depart_earliest <= depart_latest),
    CONSTRAINT route_schedules_effective CHECK (
        effective_until IS NULL OR effective_until >= effective_from
    )
);

-- ------------------------------------------------------------ occurrences ---

-- Materialized concrete trips; matching runs on these, not on templates.
--
-- Identity is (template_id, service_date) rather than the departure instant:
-- a tzdata update or DST policy change moves the instant and would silently
-- duplicate a day's trip. See RISKS.md open decision 2.
CREATE TABLE trip_occurrences (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id       uuid NOT NULL REFERENCES route_templates(id) ON DELETE CASCADE,
    service_date      date NOT NULL, -- Europe/Athens calendar date
    depart_earliest   timestamptz NOT NULL,
    depart_latest     timestamptz NOT NULL,
    seats_total       int NOT NULL CHECK (seats_total BETWEEN 1 AND 8),
    seats_free        int NOT NULL CHECK (seats_free >= 0),
    status            text NOT NULL DEFAULT 'scheduled'
                      CHECK (status IN ('scheduled','started','completed','cancelled')),
    itinerary_version int NOT NULL DEFAULT 0, -- bumps on each accepted booking

    -- Snapshot of the template at materialization time, so a later template
    -- edit cannot change what an existing booking meant.
    template_revision int NOT NULL,
    route_geom        geometry(LineString, 4326) NOT NULL,
    route_km          numeric(6,1) NOT NULL,
    route_min         int NOT NULL,
    pricing_mode      text NOT NULL
                      CHECK (pricing_mode IN ('formula','free_contribution','free')),

    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    UNIQUE (template_id, service_date),
    CONSTRAINT trip_occurrences_seats CHECK (seats_free <= seats_total),
    CONSTRAINT trip_occurrences_window CHECK (depart_earliest <= depart_latest),
    CONSTRAINT trip_occurrences_srid CHECK (ST_SRID(route_geom) = 4326)
);

CREATE INDEX trip_occurrences_status_depart ON trip_occurrences (status, depart_earliest);
CREATE INDEX trip_occurrences_geog_gist ON trip_occurrences USING GIST ((route_geom::geography));

-- --------------------------------------------------------------- requests ---

CREATE TABLE ride_requests (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    passenger_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    origin               geometry(Point, 4326) NOT NULL,
    destination          geometry(Point, 4326) NOT NULL,
    origin_label         text,
    destination_label    text,
    window_start         timestamptz NOT NULL,
    window_end           timestamptz NOT NULL,
    seats                int NOT NULL DEFAULT 1 CHECK (seats BETWEEN 1 AND 4),
    kind                 text NOT NULL CHECK (kind IN ('open','targeted')),
    target_occurrence_id uuid REFERENCES trip_occurrences(id) ON DELETE SET NULL,
    status               text NOT NULL DEFAULT 'open'
                         CHECK (status IN ('open','asking','matched','expired','cancelled')),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ride_requests_window CHECK (window_start < window_end),
    CONSTRAINT ride_requests_srid CHECK (
        ST_SRID(origin) = 4326 AND ST_SRID(destination) = 4326
    ),
    CONSTRAINT ride_requests_target CHECK (
        (kind = 'targeted' AND target_occurrence_id IS NOT NULL) OR
        (kind = 'open'     AND target_occurrence_id IS NULL)
    )
);

CREATE INDEX ride_requests_origin_gist ON ride_requests USING GIST (origin);
CREATE INDEX ride_requests_dest_gist ON ride_requests USING GIST (destination);
CREATE INDEX ride_requests_status_window ON ride_requests (status, window_end);
CREATE INDEX ride_requests_passenger ON ride_requests (passenger_id);

-- ------------------------------------------------------------------- asks ---

CREATE TABLE asks (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id          uuid NOT NULL REFERENCES ride_requests(id) ON DELETE CASCADE,
    occurrence_id       uuid NOT NULL REFERENCES trip_occurrences(id) ON DELETE CASCADE,
    detour_min          numeric(4,1) NOT NULL CHECK (detour_min >= 0),
    detour_km           numeric(5,1) NOT NULL CHECK (detour_km >= 0),
    pickup_point        geometry(Point, 4326) NOT NULL,
    dropoff_point       geometry(Point, 4326) NOT NULL,
    suggested_price_eur numeric(5,2) NOT NULL CHECK (suggested_price_eur >= 0),
    itinerary_version   int NOT NULL, -- version the detour was computed against
    status              text NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','accepted','declined','expired','cancelled')),
    expires_at          timestamptz NOT NULL,
    responded_at        timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT asks_srid CHECK (
        ST_SRID(pickup_point) = 4326 AND ST_SRID(dropoff_point) = 4326
    )
);

-- Exactly one live ask per request: this is the final backstop behind the
-- request row lock that serialises the sequential ask flow.
CREATE UNIQUE INDEX asks_one_pending ON asks (request_id) WHERE status = 'pending';
CREATE INDEX asks_pending_expiry ON asks (expires_at) WHERE status = 'pending';
CREATE INDEX asks_occurrence ON asks (occurrence_id);
-- Candidate history: a driver already asked about a request is never re-asked,
-- even after a re-rank or a restart.
CREATE UNIQUE INDEX asks_request_occurrence ON asks (request_id, occurrence_id);

-- --------------------------------------------------------------- bookings ---

CREATE TABLE bookings (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    occurrence_id       uuid NOT NULL REFERENCES trip_occurrences(id) ON DELETE CASCADE,
    request_id          uuid NOT NULL REFERENCES ride_requests(id) ON DELETE CASCADE,
    passenger_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ask_id              uuid NOT NULL REFERENCES asks(id),
    seats               int NOT NULL CHECK (seats BETWEEN 1 AND 4),
    price_eur           numeric(5,2) NOT NULL CHECK (price_eur >= 0), -- frozen at accept
    pickup_point        geometry(Point, 4326) NOT NULL,
    dropoff_point       geometry(Point, 4326) NOT NULL,
    status              text NOT NULL DEFAULT 'confirmed'
                        CHECK (status IN ('confirmed','cancelled_by_passenger',
                                          'cancelled_by_driver','completed','no_show')),
    driver_confirmed    boolean NOT NULL DEFAULT false,
    passenger_confirmed boolean NOT NULL DEFAULT false,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    UNIQUE (occurrence_id, passenger_id),
    -- One request yields at most one live booking.
    CONSTRAINT bookings_srid CHECK (
        ST_SRID(pickup_point) = 4326 AND ST_SRID(dropoff_point) = 4326
    )
);

CREATE UNIQUE INDEX bookings_one_live_per_request ON bookings (request_id)
    WHERE status IN ('confirmed','completed');
CREATE INDEX bookings_occurrence ON bookings (occurrence_id);
CREATE INDEX bookings_passenger ON bookings (passenger_id);

-- ---------------------------------------------------------------- ratings ---

CREATE TABLE ratings (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id uuid NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
    rater_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ratee_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    stars      int NOT NULL CHECK (stars BETWEEN 1 AND 5),
    tag        text,
    created_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (booking_id, rater_id),
    CONSTRAINT ratings_not_self CHECK (rater_id <> ratee_id)
);

CREATE INDEX ratings_ratee ON ratings (ratee_id);

-- ------------------------------------------------------------- social ---

CREATE TABLE trip_messages (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    occurrence_id uuid NOT NULL REFERENCES trip_occurrences(id) ON DELETE CASCADE,
    sender_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body          text NOT NULL CHECK (length(body) BETWEEN 1 AND 2000),
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- Stable pagination cursor: (created_at, id) is total, so pages cannot repeat
-- or skip a message.
CREATE INDEX trip_messages_thread ON trip_messages (occurrence_id, created_at, id);

-- Blocks are enforced during matching and ask creation, not only in chat.
CREATE TABLE blocks (
    blocker_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (blocker_id, blocked_id),
    CONSTRAINT blocks_not_self CHECK (blocker_id <> blocked_id)
);

CREATE INDEX blocks_blocked ON blocks (blocked_id);

CREATE TABLE reports (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id  uuid NOT NULL REFERENCES users(id),
    reported_id  uuid NOT NULL REFERENCES users(id),
    booking_id   uuid REFERENCES bookings(id) ON DELETE SET NULL,
    reason       text NOT NULL CHECK (length(reason) BETWEEN 1 AND 2000),
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT reports_not_self CHECK (reporter_id <> reported_id)
);

CREATE INDEX reports_reported ON reports (reported_id);

-- ----------------------------------------------------------------- outbox ---

-- Side effects are enqueued in the same transaction as the state change, then
-- delivered by a leased dispatcher. Delivery is at-least-once: a worker can die
-- between the provider accepting and the row being marked done. See RISKS.md
-- open decision 5.
CREATE TABLE outbox (
    id              bigserial PRIMARY KEY,
    kind            text NOT NULL, -- 'push','ws','ask_next','materialize',...
    payload         jsonb NOT NULL,
    idempotency_key text NOT NULL UNIQUE,
    run_after       timestamptz NOT NULL DEFAULT now(),
    attempts        int NOT NULL DEFAULT 0,
    locked_by       text,
    locked_until    timestamptz,
    done_at         timestamptz,
    dead_at         timestamptz,
    last_error      text,
    created_at      timestamptz NOT NULL DEFAULT now()
);

-- The dispatcher's claim query: pending rows ordered by (run_after, id).
CREATE INDEX outbox_claimable ON outbox (run_after, id)
    WHERE done_at IS NULL AND dead_at IS NULL;

-- +goose Down

DROP TABLE IF EXISTS outbox;
DROP TABLE IF EXISTS reports;
DROP TABLE IF EXISTS blocks;
DROP TABLE IF EXISTS trip_messages;
DROP TABLE IF EXISTS ratings;
DROP TABLE IF EXISTS bookings;
DROP TABLE IF EXISTS asks;
DROP TABLE IF EXISTS ride_requests;
DROP TABLE IF EXISTS trip_occurrences;
DROP TABLE IF EXISTS route_schedules;
DROP TABLE IF EXISTS route_templates;
DROP TABLE IF EXISTS push_tokens;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS otp_codes;
DROP TABLE IF EXISTS driver_profiles;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS regions;

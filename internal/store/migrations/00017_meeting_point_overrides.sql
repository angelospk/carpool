-- +goose Up
-- Correcting the automatic meeting points, and letting people propose new ones.
--
-- The harvested list is good in villages and poor in cities. Θεσσαλονίκη's
-- three points come out as coach-ticket agencies tagged amenity=bus_station;
-- Αλεξανδρούπολη gets urban bus-line termini rather than the ΚΤΕΛ. No ranking
-- fixes that, because the fact that one is a ticket office and the other is the
-- bus station is not in the tags. It needs a person.
--
-- Two tables, not one. The first is what somebody asked for and what was
-- decided about it; the second is what is true right now. Collapsing them means
-- every public read has to interpret moderation history, and reversing a
-- decision either destroys the history or adds a row whose precedence against
-- its predecessor is undefined.

CREATE TABLE moderator_grants (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    granted    boolean NOT NULL,
    -- Null when it came from the operator command rather than from another
    -- moderator, which is how the first one has to be made.
    actor_id   uuid REFERENCES users (id) ON DELETE SET NULL,
    note       text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX moderator_grants_user ON moderator_grants (user_id, created_at DESC);

-- Checked on every moderated request rather than carried in the token: a
-- revoked moderator has to stop being one now, not when their access token
-- expires.
ALTER TABLE users ADD COLUMN is_moderator boolean NOT NULL DEFAULT false;

-- Everything anybody asked for, and what became of it. Kept forever: it is the
-- record of who decided what, and a moderation system without one is a rumour.
CREATE TABLE meeting_point_requests (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The settlement, as places.Place.ID: "node/139260064".
    place_id    text NOT NULL,
    action      text NOT NULL CHECK (action IN ('add', 'update', 'hide')),
    -- The point being changed, as its stable public id. Null only for 'add',
    -- which is creating one.
    target_id   text,
    name        text,
    kind        text CHECK (kind IN ('bus_station','train_station','port','fuel','parking','other')),
    point       geometry(Point, 4326),
    status      text NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'approved', 'rejected')),
    proposed_by uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    reviewed_by uuid REFERENCES users (id) ON DELETE SET NULL,
    reviewed_at timestamptz,
    -- Shown to the person who asked, so a rejection is an answer rather than
    -- silence.
    reason      text,
    -- A retry after a timeout on a phone must not become a second proposal.
    idempotency_key text,
    created_at  timestamptz NOT NULL DEFAULT now(),

    -- Adding a point means saying what and where; hiding one means saying which.
    CONSTRAINT mpr_add_is_complete CHECK (
        action <> 'add' OR (target_id IS NULL AND name IS NOT NULL AND kind IS NOT NULL AND point IS NOT NULL)),
    CONSTRAINT mpr_edit_names_a_target CHECK (
        action = 'add' OR target_id IS NOT NULL),
    CONSTRAINT mpr_hide_carries_nothing CHECK (
        action <> 'hide' OR (name IS NULL AND kind IS NULL AND point IS NULL)),
    -- A decision has a decider and a moment, and an undecided request has
    -- neither. Without this the two drift apart and the audit stops meaning
    -- anything.
    CONSTRAINT mpr_decision_is_complete CHECK (
        (status = 'pending' AND reviewed_by IS NULL AND reviewed_at IS NULL)
        OR (status <> 'pending' AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL)),
    CONSTRAINT mpr_srid CHECK (point IS NULL OR ST_SRID(point) = 4326)
);

CREATE INDEX meeting_point_requests_pending
    ON meeting_point_requests (created_at) WHERE status = 'pending';
CREATE INDEX meeting_point_requests_author
    ON meeting_point_requests (proposed_by, created_at DESC);
-- The daily limit counts a person's recent requests, whatever became of them.
CREATE INDEX meeting_point_requests_recent
    ON meeting_point_requests (proposed_by, created_at);
CREATE UNIQUE INDEX meeting_point_requests_idempotent
    ON meeting_point_requests (proposed_by, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- What is true now. One row per point that differs from the harvested list.
CREATE TABLE meeting_point_overrides (
    -- The stable public id of the point: "node/123456" for one that came out of
    -- the harvest, "curated/<uuid>" for one added here. The two cannot collide,
    -- because an OSM id is a number.
    target_id  text PRIMARY KEY,
    place_id   text NOT NULL,
    action     text NOT NULL CHECK (action IN ('add', 'update', 'hide')),
    name       text,
    kind       text CHECK (kind IN ('bus_station','train_station','port','fuel','parking','other')),
    point      geometry(Point, 4326),
    -- Lower sorts first, and a curated point can therefore be put above the
    -- automatic ones — which is the entire reason a moderator would add the
    -- ΚΤΕΛ to a city that already has three bad points.
    priority   integer NOT NULL DEFAULT 0,
    request_id uuid REFERENCES meeting_point_requests (id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT mpo_add_is_complete CHECK (
        action <> 'add' OR (name IS NOT NULL AND kind IS NOT NULL AND point IS NOT NULL)),
    CONSTRAINT mpo_hide_carries_nothing CHECK (
        action <> 'hide' OR (name IS NULL AND kind IS NULL AND point IS NULL)),
    CONSTRAINT mpo_srid CHECK (point IS NULL OR ST_SRID(point) = 4326)
);

CREATE INDEX meeting_point_overrides_place ON meeting_point_overrides (place_id, priority, target_id);

-- +goose Down
DROP TABLE meeting_point_overrides;
DROP TABLE meeting_point_requests;
ALTER TABLE users DROP COLUMN is_moderator;
DROP TABLE moderator_grants;

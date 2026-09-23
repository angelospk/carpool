-- +goose Up

-- Groups: the people a passenger already knows by where they work or study.
--
-- A stranger's phone number and photo are thin grounds for getting into their
-- car. "She works at the hospital too" is not, and it is the signal every
-- commuter service that worked was built on: they signed an employer, and the
-- employer's people carpooled with each other. A group is that, without a
-- contract: somebody creates it, hands the code round, and members can see who
-- else is in it and publish routes only they are offered.

CREATE TABLE groups (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL CHECK (char_length(name) BETWEEN 2 AND 60),
    -- The code is how somebody joins. It can be replaced by an admin, which is
    -- the answer to it being screenshotted into the wrong chat.
    join_code  text NOT NULL UNIQUE CHECK (join_code = upper(join_code) AND length(join_code) BETWEEN 6 AND 16),
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Membership is a history, not a set. Leaving sets left_at instead of deleting
-- the row, because what an organisation may be shown later — see the monthly
-- export — depends on who was a member on the day a trip ran, not on who is
-- a member on the day somebody asks.
CREATE TABLE group_members (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id  uuid NOT NULL REFERENCES groups(id),
    user_id   uuid NOT NULL REFERENCES users(id),
    role      text NOT NULL CHECK (role IN ('admin', 'member')),
    joined_at timestamptz NOT NULL,
    left_at   timestamptz,
    CHECK (left_at IS NULL OR left_at >= joined_at)
);

-- One live membership per person per group. Earlier, ended ones stay beside it.
CREATE UNIQUE INDEX group_members_one_live ON group_members (group_id, user_id) WHERE left_at IS NULL;
CREATE INDEX group_members_by_user ON group_members (user_id) WHERE left_at IS NULL;

-- Wrong codes, counted per person. The code is eight characters from a
-- 26-letter alphabet; without a ceiling on guesses that is a search, not a
-- secret.
CREATE TABLE group_join_failures (
    user_id uuid NOT NULL REFERENCES users(id),
    at      timestamptz NOT NULL
);
CREATE INDEX group_join_failures_by_user ON group_join_failures (user_id, at);

-- A route can be offered to one group only. Null is everybody, which is what
-- every route published before this column existed meant.
--
-- No ON DELETE: groups are not deleted, and if that ever changes, a restricted
-- route quietly becoming public is the wrong way for it to go.
ALTER TABLE route_templates ADD COLUMN group_id uuid REFERENCES groups(id);

-- +goose Down
ALTER TABLE route_templates DROP COLUMN group_id;
DROP TABLE group_join_failures;
DROP TABLE group_members;
DROP TABLE groups;

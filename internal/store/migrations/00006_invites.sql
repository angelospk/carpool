-- +goose Up

-- Invite codes, for a pilot that has to start somewhere.
--
-- The point is not scarcity, it is provenance: if everybody arrives through
-- somebody, then everybody is one hop from a person another person vouched for.
-- In one prefecture that is a stronger safety signal than any profile badge,
-- and it is the only one the app can actually verify, because it is the app
-- that issued the code.
CREATE TABLE invite_codes (
    code       text PRIMARY KEY CHECK (code = upper(code) AND length(code) BETWEEN 6 AND 16),
    -- Null for the codes seeded by hand to start the pilot: they came from
    -- nobody inside the system.
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    max_uses   int NOT NULL DEFAULT 5 CHECK (max_uses > 0),
    used_count int NOT NULL DEFAULT 0 CHECK (used_count >= 0),
    expires_at timestamptz,
    disabled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),

    -- Enforced here as well as in the code that increments it, because a use
    -- count that can pass its limit is the one bug this table exists to prevent.
    CONSTRAINT invite_codes_within_limit CHECK (used_count <= max_uses)
);

CREATE INDEX invite_codes_owner ON invite_codes (created_by);

-- Who came in through whom. This is the chain that makes the vouching real, and
-- the thing to read when someone has to be traced back.
CREATE TABLE invite_redemptions (
    code        text NOT NULL REFERENCES invite_codes(code) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    redeemed_at timestamptz NOT NULL,

    -- One account, one way in.
    PRIMARY KEY (user_id)
);

CREATE INDEX invite_redemptions_code ON invite_redemptions (code);

-- +goose Down

DROP TABLE invite_redemptions;
DROP TABLE invite_codes;

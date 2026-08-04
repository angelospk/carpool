-- +goose Up
-- Changing the number the account is reached at.
--
-- The phone number is the credential. Everything else about this table follows
-- from that: getting it wrong once turns an account into somebody else's.
--
-- A challenge, not a loose code. Confirmation quotes a challenge id and a code,
-- and the number being moved to comes out of the row rather than out of the
-- request. If the caller could name the target at confirmation time, a code
-- sent to one number could be spent against another.
--
-- The code is stored as an HMAC under the server key, not as a bare hash. Six
-- digits is a million possibilities, which any laptop walks through in seconds
-- against SHA-256; the key is what makes a leaked table useless on its own.

CREATE TABLE phone_change_challenges (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- Recorded so the swap can check the account has not moved underneath the
    -- challenge: two changes started at once must not both apply.
    old_phone    text NOT NULL,
    new_phone    text NOT NULL,
    code_hmac    bytea NOT NULL,
    attempts     integer NOT NULL DEFAULT 0,
    expires_at   timestamptz NOT NULL,
    consumed_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT phone_change_new_e164 CHECK (new_phone ~ '^\+[1-9][0-9]{7,14}$'),
    CONSTRAINT phone_change_hmac_len CHECK (octet_length(code_hmac) = 32)
);

-- Finding a user's live challenges, which happens on every request and every
-- confirmation.
CREATE INDEX phone_change_by_user ON phone_change_challenges (user_id, created_at DESC);

-- Rate limiting is per target number as well as per user. Without the first, an
-- authenticated user can send unlimited SMS to any number they like by asking
-- to move to it; without the second, one account can grind through the whole
-- SMS budget.
CREATE INDEX phone_change_by_target ON phone_change_challenges (new_phone, created_at DESC);

-- The rate-limit count filters on created_at first and then counts by user and
-- by target in one pass, so neither index above leads with the column it
-- actually narrows on.
CREATE INDEX phone_change_recent ON phone_change_challenges (created_at DESC);

-- +goose Down
DROP TABLE phone_change_challenges;

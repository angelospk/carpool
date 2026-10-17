-- +goose Up
-- The code that has to accompany DELETE /v1/me.
--
-- Deleting an account used to need only the access token, so a stolen or
-- borrowed session could erase it for good (QA 2026-10-17 A14). The code is
-- sent by SMS to the account's own number and spent by the deletion.
--
-- Shaped like phone_change_challenges, and kept apart from it on purpose: a
-- person asking for a delete code must not eat into, or be locked out of, the
-- limits that guard a change of number.
--
-- Stored as an HMAC under the server key rather than a bare hash: six digits is
-- a million possibilities, which a laptop walks through in seconds against
-- SHA-256.

CREATE TABLE account_delete_codes (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- The number the code was sent to. A code only counts while the account
    -- still has that number.
    phone        text NOT NULL,
    code_hmac    bytea NOT NULL,
    attempts     integer NOT NULL DEFAULT 0,
    expires_at   timestamptz NOT NULL,
    consumed_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT account_delete_codes_hmac_len CHECK (octet_length(code_hmac) = 32)
);

-- Finding a user's live code, and counting their recent requests.
CREATE INDEX account_delete_codes_by_user ON account_delete_codes (user_id, created_at DESC);

-- +goose Down
DROP TABLE account_delete_codes;

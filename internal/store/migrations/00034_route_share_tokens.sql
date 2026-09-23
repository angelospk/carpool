-- +goose Up

-- A link a driver can paste instead of a sentence. The token is 128 random
-- bits, so a link cannot be guessed; it can be withdrawn, which clears it, and
-- a new one is a new token, so a link that travelled somewhere unwanted stops
-- working the moment it is replaced.
--
-- Stored as it is rather than hashed, because the driver can open "share"
-- again next week and should get the same link back rather than invalidating
-- the one already in the village chat.
ALTER TABLE route_templates ADD COLUMN share_token text UNIQUE
    CHECK (share_token IS NULL OR length(share_token) BETWEEN 22 AND 64);

-- +goose Down
ALTER TABLE route_templates DROP COLUMN share_token;

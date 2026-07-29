-- +goose Up

-- Refresh rotation is single-use, but the client can only persist the new token
-- after the response arrives. A lost response therefore left the client holding
-- a spent token, and the next launch looked exactly like a replay.
--
-- These two columns record *which* request consumed a token and what it minted,
-- so a retry of that same request can be told apart from a replay of a stolen
-- token and handed back the same successor.
ALTER TABLE refresh_tokens
    ADD COLUMN attempt_id  uuid,
    ADD COLUMN replaced_by uuid REFERENCES refresh_tokens(id) ON DELETE SET NULL;

-- A token is consumed by exactly one attempt, and mints exactly one successor.
-- Without this a concurrent burst could chain several successors onto one
-- parent, and every retry would invalidate the last one's answer.
CREATE UNIQUE INDEX refresh_tokens_one_successor ON refresh_tokens (replaced_by)
    WHERE replaced_by IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS refresh_tokens_one_successor;
ALTER TABLE refresh_tokens DROP COLUMN IF EXISTS replaced_by, DROP COLUMN IF EXISTS attempt_id;

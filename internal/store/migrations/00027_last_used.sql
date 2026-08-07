-- +goose Up

-- When a push token was last any use.
--
-- The privacy policy promises "tokens ειδοποιήσεων — μέχρι την απενεργοποίηση,
-- το πολύ 24 μήνες από την τελευταία χρήση", and nothing recorded the last use,
-- so the second half of that sentence could not be enforced at all.
--
-- Nullable, and read as COALESCE(last_used_at, created_at) rather than
-- defaulted. A predicate on a nullable column is a row that lives for ever:
-- `last_used_at < cutoff` is unknown when it is null, and unknown is not true,
-- so every token that predates this column would be immortal. Codex flagged
-- exactly that shape.
--
-- Deliberately NOT backfilled to created_at. Every token in existence was
-- created within the last few weeks of development, so the two are the same
-- thing today; backfilling would only matter on a deployment with real history,
-- and there the honest answer is that the true last use cannot be reconstructed.
-- The COALESCE handles it either way without inventing a fact.
ALTER TABLE push_tokens ADD COLUMN last_used_at timestamptz;

-- The janitor scans by age across all rows, so the age is what it is indexed
-- by. Expression index because the predicate is the expression: a plain index on
-- last_used_at would not be usable for the COALESCE.
CREATE INDEX push_tokens_last_use ON push_tokens (COALESCE(last_used_at, created_at));

-- Reports and blocks are swept by age too, and neither had an index for it.
-- Both tables are small now and would not be for ever; a sequential scan every
-- hour is the kind of thing nobody notices until it is the reason the database
-- is busy.
CREATE INDEX reports_created_at ON reports (created_at);
CREATE INDEX blocks_created_at  ON blocks  (created_at);

-- +goose Down

DROP INDEX IF EXISTS blocks_created_at;
DROP INDEX IF EXISTS reports_created_at;
DROP INDEX IF EXISTS push_tokens_last_use;
ALTER TABLE push_tokens DROP COLUMN IF EXISTS last_used_at;

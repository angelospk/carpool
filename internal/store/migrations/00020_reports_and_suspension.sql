-- +goose Up

-- Reports have been written since the first migration and read by nothing.
--
-- A report button that files into a table nobody opens is worse than no button:
-- it tells somebody their complaint has been received when what has happened is
-- that a row exists. Now there is a queue, a decision, and a record of who made
-- it.
ALTER TABLE reports
    ADD COLUMN status      text NOT NULL DEFAULT 'open'
                CHECK (status IN ('open', 'dismissed', 'actioned')),
    ADD COLUMN reviewed_by uuid REFERENCES users(id),
    ADD COLUMN reviewed_at timestamptz,
    ADD COLUMN resolution  text CHECK (resolution IS NULL OR length(resolution) BETWEEN 1 AND 1000);

-- An open report has no reviewer and a closed one does. Without this the two
-- halves can drift: a status of 'dismissed' with nobody's name on it, or a
-- reviewer recorded against a report still sitting in the queue.
ALTER TABLE reports ADD CONSTRAINT reports_review_consistent CHECK (
    (status =  'open' AND reviewed_by IS     NULL AND reviewed_at IS     NULL AND resolution IS NULL) OR
    (status <> 'open' AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL));

-- The queue itself: oldest first, and only the open ones. Ordered on (created_at,
-- id) rather than created_at alone so that paging through it is stable when two
-- reports land in the same microsecond.
CREATE INDEX reports_open ON reports (created_at, id) WHERE status = 'open';

-- "How many times has this person been reported before" is the first thing a
-- moderator needs and the reason a single report is rarely enough to act on.
CREATE INDEX reports_reported_recent ON reports (reported_id, created_at DESC);
-- And the daily limit, which is what stops one person filling the queue.
CREATE INDEX reports_reporter_recent ON reports (reporter_id, created_at DESC);

-- What was done to an account, by whom, and why.
--
-- users.status says what is true now. This says how it got that way, which is
-- the part somebody appealing needs and the part a moderator reviewing another
-- moderator needs. Append-only, like reports.
CREATE TABLE account_actions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action     text NOT NULL CHECK (action IN ('suspend', 'reinstate')),
    reason     text NOT NULL CHECK (length(reason) BETWEEN 1 AND 1000),
    -- Null when the app did it to itself rather than a person doing it. Nothing
    -- does that today; the column is nullable so that when something does, the
    -- honest answer is available.
    actor_id   uuid REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX account_actions_user ON account_actions (user_id, created_at DESC);

-- +goose Down

DROP TABLE IF EXISTS account_actions;
DROP INDEX IF EXISTS reports_reporter_recent;
DROP INDEX IF EXISTS reports_reported_recent;
DROP INDEX IF EXISTS reports_open;
ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_review_consistent;
ALTER TABLE reports
    DROP COLUMN status,
    DROP COLUMN reviewed_by,
    DROP COLUMN reviewed_at,
    DROP COLUMN resolution;

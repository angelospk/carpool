-- +goose Up

-- run_after decides when the dispatcher may claim a row, and the dispatcher
-- compares it against its injected clock. A column default fills it from the
-- database clock instead, and the two agree only by accident: a row inserted at
-- the database's 19:44 is due in the future to a dispatcher held at noon, and is
-- silently never delivered. Dropping the default turns a producer that forgets
-- the due time into a failed insert instead of a lost notification.
ALTER TABLE outbox ALTER COLUMN run_after DROP DEFAULT;

-- The idempotency key is what both the queue and the app deduplicate on. A
-- blank one collides with every other blank one, so ON CONFLICT DO NOTHING
-- swallows the second effect and reports success.
ALTER TABLE outbox ADD CONSTRAINT outbox_key_not_blank CHECK (idempotency_key <> '');

-- +goose Down

-- The default is deliberately not restored. Rolling this migration back must
-- not reinstate a column default that hands the due time to the wrong clock;
-- the code that reads run_after has no rollback.
ALTER TABLE outbox DROP CONSTRAINT outbox_key_not_blank;

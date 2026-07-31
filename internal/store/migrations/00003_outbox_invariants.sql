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

-- This migration is irreversible on purpose, and says so rather than reporting
-- a rollback it did not perform. Restoring the default would reinstate a column
-- that hands the due time to the wrong clock; silently leaving it off would
-- claim a schema the down migration did not produce. Whoever genuinely needs to
-- go back can drop the constraint by hand and decide about the default.
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'migration 00003 is irreversible: restoring run_after DEFAULT now() would reintroduce the cross-clock bug';
END $$;
-- +goose StatementEnd

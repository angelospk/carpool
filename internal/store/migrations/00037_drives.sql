-- +goose Up
-- A drive: the driver has said when they are really leaving, and the
-- departure window has been narrowed to that minute. drive_set_at says it
-- happened; scheduled_* keep the window the schedule gave, so a second change
-- of mind is measured against what was published rather than against the
-- first change. The materializer leaves a drive alone.
ALTER TABLE trip_occurrences
    ADD COLUMN drive_set_at       timestamptz NULL,
    ADD COLUMN scheduled_earliest timestamptz NULL,
    ADD COLUMN scheduled_latest   timestamptz NULL,
    ADD COLUMN nudged_at          timestamptz NULL;

-- Whether the driver wants to be told, as a departure comes up, that somebody
-- on their road is waiting. Off until they turn it on.
ALTER TABLE route_templates ADD COLUMN nudge_when_leaving boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE route_templates DROP COLUMN nudge_when_leaving;
ALTER TABLE trip_occurrences
    DROP COLUMN nudged_at,
    DROP COLUMN scheduled_latest,
    DROP COLUMN scheduled_earliest,
    DROP COLUMN drive_set_at;

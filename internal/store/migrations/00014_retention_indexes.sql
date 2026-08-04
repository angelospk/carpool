-- +goose Up
-- Indexes the retention janitor scans on.
--
-- Without these, every pass sequentially scans the whole table to find the few
-- rows old enough to delete, and the cost grows with the data rather than with
-- the amount being removed. A janitor whose cost rises forever is a janitor
-- somebody eventually turns off.
--
-- phone_change_challenges already has phone_change_recent (created_at DESC),
-- and trip_messages already has trip_messages_thread (occurrence_id,
-- created_at, id), which answers "has this thread been silent". Neither is
-- repeated here.

CREATE INDEX otp_codes_created ON otp_codes (created_at);

-- Two indexes rather than one over both columns: the janitor asks the two
-- questions separately, because an OR across two nullable timestamps cannot use
-- either index. Only the second is partial — every row has an expires_at, while
-- revoked_at is null for most, so excluding those keeps that index small.
CREATE INDEX refresh_tokens_expiry ON refresh_tokens (expires_at);
CREATE INDEX refresh_tokens_revoked ON refresh_tokens (revoked_at) WHERE revoked_at IS NOT NULL;

-- The message rule starts from occurrences that departed long ago, regardless
-- of what became of them, so the existing (status, depart_earliest) index does
-- not serve it.
CREATE INDEX trip_occurrences_depart ON trip_occurrences (depart_earliest);

-- +goose Down
DROP INDEX otp_codes_created;
DROP INDEX refresh_tokens_expiry;
DROP INDEX refresh_tokens_revoked;
DROP INDEX trip_occurrences_depart;

-- +goose Up

-- Every access or erasure request that was carried out, and when.
--
-- The legal advice asked for "διαδικασία χειρισμού αιτημάτων με καταγραφή
-- ενεργειών": being able to answer a request is not the same as being able to
-- show that it was answered. This is the showing.
--
-- The row survives the erasure it records, which is the point: the user_id
-- still exists after anonymisation, and what remains attached to it is a date
-- and a word, not a person.
CREATE TABLE data_requests (
    id         bigserial PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       text NOT NULL CHECK (kind IN ('access', 'erasure')),
    handled_at timestamptz NOT NULL
);

CREATE INDEX data_requests_user ON data_requests (user_id, handled_at);

-- Erasure replaces the phone number with a tombstone, and a tombstone is not a
-- phone number. The column stays unique, which is what the identifier needs,
-- but it now holds two kinds of value and the constraint has to say so rather
-- than being quietly widened to accept anything.
ALTER TABLE users DROP CONSTRAINT users_phone_e164;
ALTER TABLE users ADD CONSTRAINT users_phone_e164 CHECK (
    phone ~ '^\+[1-9][0-9]{7,14}$'
    OR (status = 'deleted' AND phone ~ '^deleted:[0-9a-f-]{36}$')
);

-- +goose Down

DROP TABLE data_requests;
ALTER TABLE users DROP CONSTRAINT users_phone_e164;
ALTER TABLE users ADD CONSTRAINT users_phone_e164 CHECK (phone ~ '^\+[1-9][0-9]{7,14}$');

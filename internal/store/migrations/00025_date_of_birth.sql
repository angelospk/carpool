-- +goose Up

-- How old somebody is, asked once, at the door.
--
-- The app had no age check at all. Every Greek carpooling service has one:
-- GoRide's terms say "οι χρήστες της υπηρεσίας πρέπει να είναι ενήλικοι - 18
-- ετών", and there is no version of this product where a fourteen-year-old
-- getting into a stranger's car is a thing the app helped with.
--
-- NOT NULL with no default and no backfill, because there is nothing honest to
-- backfill with. Nobody's age can be inferred from what the app already knows,
-- and a sentinel date would be an age gate that waves through everybody who
-- signed up before it.
--
-- That is only safe because this lands before anybody has signed up: the
-- deployment has zero accounts, verified before writing it. The guard below
-- says so out loud rather than leaving the next person to find out from a
-- constraint violation halfway through a deploy. If accounts ever do exist when
-- this runs, the answer is not a default — it is asking those people for their
-- date of birth and holding their account until they answer, which is a
-- migration this one is not.
-- +goose StatementBegin
DO $$
DECLARE existing bigint;
BEGIN
    SELECT count(*) INTO existing FROM users;
    IF existing > 0 THEN
        RAISE EXCEPTION 'migration 00025 cannot run over % existing account(s): there is no honest date of birth to backfill them with', existing;
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE users ADD COLUMN date_of_birth date NOT NULL;

-- Bounded on both ends, so a typo cannot store a date nobody was born on.
--
-- Deliberately NOT a check against being eighteen. CURRENT_DATE in a CHECK is a
-- constraint whose truth changes overnight, which makes every later ALTER TABLE
-- and every restore from backup a gamble on what day it is. The age rule lives
-- in internal/domain/age.go, where it is tested; this only rules out dates that
-- are impossible rather than dates that are too recent.
--
-- '1900-01-01' rather than a computed bound for the same reason: a fixed date is
-- still true tomorrow.
ALTER TABLE users ADD CONSTRAINT users_date_of_birth_plausible
    CHECK (date_of_birth >= DATE '1900-01-01');

-- Never selected into a public profile. There is no index and no reason for one:
-- nothing searches, sorts or filters by it, and the only read is the account's
-- own. Recorded here because "why is there no index on this" is a question
-- somebody will ask.

-- +goose Down

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_date_of_birth_plausible;
ALTER TABLE users DROP COLUMN IF EXISTS date_of_birth;

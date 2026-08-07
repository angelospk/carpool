-- +goose Up

-- A rating stops dying with the trip it came from.
--
-- The privacy policy keeps trips for 24 months and ratings for as long as the
-- account is active. Those two promises cannot both be kept while
-- ratings.booking_id is NOT NULL with ON DELETE CASCADE: deleting a
-- two-year-old departure cascades through its bookings and takes the ratings
-- with it, so a driver's reputation would quietly erode by age and the worst
-- affected would be the ones who have been here longest.
--
-- Codex flagged this reviewing the retention plan, and it is the reason the trip
-- rule is not in the previous migration.
--
-- No snapshot is needed, which is worth stating because the obvious fix is to
-- copy trip facts onto the rating. Nothing reads booking_id: every query in the
-- app selects stars, tag, created_at and the two user ids, all of which are on
-- the row already. Copying trip details onto a rating would mean holding trip
-- data for the life of the account — more retention, not less, in the name of a
-- retention rule.
ALTER TABLE ratings ALTER COLUMN booking_id DROP NOT NULL;

ALTER TABLE ratings DROP CONSTRAINT ratings_booking_id_fkey;
ALTER TABLE ratings ADD CONSTRAINT ratings_booking_id_fkey
    FOREIGN KEY (booking_id) REFERENCES bookings(id) ON DELETE SET NULL;

-- The unique constraint stays as it was, and keeps working, because Postgres
-- treats NULLs as distinct in a unique index. Its job is to stop somebody
-- rating the same live booking twice; once the booking is gone there is nothing
-- left to rate twice, and the rows that survive are history rather than
-- something anybody can add to.

-- +goose Down

-- Ratings whose booking has already been swept cannot be restored to a state
-- where booking_id is NOT NULL — there is no booking to point at. They are
-- deleted, which is the only honest way back and is why this direction loses
-- data. Rolling this back after the trip rule has run once is not a reversal.
DELETE FROM ratings WHERE booking_id IS NULL;

ALTER TABLE ratings DROP CONSTRAINT ratings_booking_id_fkey;
ALTER TABLE ratings ADD CONSTRAINT ratings_booking_id_fkey
    FOREIGN KEY (booking_id) REFERENCES bookings(id) ON DELETE CASCADE;

ALTER TABLE ratings ALTER COLUMN booking_id SET NOT NULL;

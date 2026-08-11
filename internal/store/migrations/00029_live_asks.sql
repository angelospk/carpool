-- +goose Up

-- The candidate filter now subtracts seats already promised to somebody from
-- each occurrence's free seats, which means it runs a count over pending asks
-- once per candidate rather than once per search.
--
-- asks_occurrence (occurrence_id) already exists and would serve it, but it
-- covers every ask a trip has ever had. A trip that has been offered around for
-- weeks accumulates declined and expired rows, and the count has to walk all of
-- them to find the handful that are still standing. The partial index holds
-- only the live ones, which is both the smaller set and the one that stays
-- small as the table grows.
--
-- expires_at is in the index rather than only the predicate because the count
-- reads it: a pending ask whose expiry has passed still counts as pending until
-- something sweeps it, so the age is part of the question, not a leftover.
CREATE INDEX asks_occurrence_live ON asks (occurrence_id, expires_at)
    WHERE status = 'pending';

-- +goose Down

DROP INDEX IF EXISTS asks_occurrence_live;

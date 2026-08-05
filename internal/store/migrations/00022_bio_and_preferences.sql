-- +goose Up

-- Two lines about yourself, and what the ride is like.
--
-- Somebody deciding whether to get into a stranger's car has a photo, a name
-- and a rating. All three say who; none says what the two hours will be like.
-- A sentence and a handful of tags do, and they are the cheapest trust signal
-- there is.
--
-- The regex is not decoration: NormaliseBio trims before writing, so a bio of
-- three spaces cannot arrive through the API — but the column is also written
-- by hand during support and by whatever comes next, and "not empty" is a rule
-- about the data rather than about one code path.
ALTER TABLE users ADD COLUMN bio text
    CHECK (bio IS NULL OR (char_length(bio) BETWEEN 1 AND 500 AND bio ~ '[^[:space:]]'));

-- Preferences are a closed vocabulary, not free tags.
--
-- Free tags cannot be translated, cannot be filtered on later, and turn into a
-- second bio nobody moderates. A fixed list can be shown in Greek or English
-- from the same row, and a tag that turns out to matter can become a matching
-- rule without a data migration.
--
-- They describe, they do not enforce. "Δεν καπνίζω" is what to expect, not a
-- promise the app polices — and a passenger who minds can ask.
CREATE TABLE user_preferences (
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tag        text NOT NULL CHECK (tag IN (
                   'no_smoking', 'smoking_ok',
                   'pets_ok', 'no_pets',
                   'music_on', 'quiet_ride',
                   'chatty', 'keep_to_myself',
                   'air_conditioning',
                   'big_luggage_ok', 'cabin_bag_only'
               )),
    created_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, tag)
);

-- +goose Down

DROP TABLE IF EXISTS user_preferences;
ALTER TABLE users DROP COLUMN IF EXISTS bio;

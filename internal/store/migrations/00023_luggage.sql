-- +goose Up

-- What fits, said before anybody sets off.
--
-- The failure this exists to stop is somebody standing at the kerb with a
-- bicycle beside a driver who has a boot full of their own things. Both of them
-- were reasonable; nobody asked.
--
-- The order is the feature. A driver who will take a suitcase will take a cabin
-- bag, and one who will take neither will not discover it at the pickup — see
-- internal/domain/luggage.go, where the comparison lives.
CREATE TYPE luggage_size AS ENUM ('none', 'cabin', 'suitcase', 'bulky');

-- On the route rather than on the car, for the same reason the detour budget is
-- on the route: "today the boot is full" is a statement about today, and a car
-- that could take a bicycle in March is not offering to in April.
--
-- 'suitcase' as the default, because that is what an ordinary boot takes and
-- because a default of 'none' would silently make every existing route refuse
-- everybody the moment matching started reading this column.
ALTER TABLE route_templates
    ADD COLUMN luggage luggage_size NOT NULL DEFAULT 'suitcase';

-- Copied onto each departure like every other thing the matcher reads, so that
-- editing a route does not change what a trip somebody already booked agreed to
-- carry.
ALTER TABLE trip_occurrences
    ADD COLUMN luggage luggage_size NOT NULL DEFAULT 'suitcase';

UPDATE trip_occurrences o SET luggage = t.luggage
  FROM route_templates t WHERE t.id = o.template_id;

-- And what the passenger is bringing. 'none' by default: most people bring a
-- bag they can hold, and the one who is bringing skis is the one who will
-- happily say so.
ALTER TABLE ride_requests
    ADD COLUMN luggage luggage_size NOT NULL DEFAULT 'none';

-- The two preference tags this replaces.
--
-- They asked the same question in a second place and could disagree with it: a
-- profile saying "big bags fine" beside a route that takes a cabin bag is two
-- answers, and a passenger reading the friendlier one turns up with a suitcase.
-- Preferences describe what somebody is like; this decides what happens.
-- Kept rather than dropped, so that rolling this migration back restores what
-- people actually chose. The rows are obsolete, not worthless: a down migration
-- that silently loses somebody's answers is the kind of thing nobody notices
-- until they are restoring a backup at two in the morning.
CREATE TABLE user_preferences_luggage_backup AS
    SELECT * FROM user_preferences WHERE tag IN ('big_luggage_ok', 'cabin_bag_only');

DELETE FROM user_preferences WHERE tag IN ('big_luggage_ok', 'cabin_bag_only');

ALTER TABLE user_preferences DROP CONSTRAINT user_preferences_tag_check;
ALTER TABLE user_preferences ADD CONSTRAINT user_preferences_tag_check CHECK (tag IN (
    'no_smoking', 'smoking_ok',
    'pets_ok', 'no_pets',
    'music_on', 'quiet_ride',
    'chatty', 'keep_to_myself',
    'air_conditioning'
));

-- +goose Down

ALTER TABLE user_preferences DROP CONSTRAINT user_preferences_tag_check;
ALTER TABLE user_preferences ADD CONSTRAINT user_preferences_tag_check CHECK (tag IN (
    'no_smoking', 'smoking_ok',
    'pets_ok', 'no_pets',
    'music_on', 'quiet_ride',
    'chatty', 'keep_to_myself',
    'air_conditioning',
    'big_luggage_ok', 'cabin_bag_only'
));

INSERT INTO user_preferences SELECT * FROM user_preferences_luggage_backup
    ON CONFLICT DO NOTHING;
DROP TABLE IF EXISTS user_preferences_luggage_backup;

ALTER TABLE ride_requests     DROP COLUMN IF EXISTS luggage;
ALTER TABLE trip_occurrences  DROP COLUMN IF EXISTS luggage;
ALTER TABLE route_templates   DROP COLUMN IF EXISTS luggage;
DROP TYPE IF EXISTS luggage_size;

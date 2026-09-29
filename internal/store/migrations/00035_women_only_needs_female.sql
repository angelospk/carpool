-- +goose Up
-- "Women drivers only" is a setting a woman turns on for herself. It was
-- stored for anybody and read by nothing, so the profile showed it switched on
-- while the matcher offered every driver there was. The matcher reads it now;
-- this makes sure what it reads means one thing.
UPDATE users SET women_only = false WHERE women_only AND gender IS DISTINCT FROM 'female';
ALTER TABLE users ADD CONSTRAINT users_women_only_female
    CHECK (NOT women_only OR gender = 'female');

-- +goose Down
ALTER TABLE users DROP CONSTRAINT users_women_only_female;

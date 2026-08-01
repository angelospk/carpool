-- +goose Up

-- Where else this person can be found, in their own words.
--
-- Not a login and not a verification: anyone can paste anyone's profile, and
-- the app says so where it shows it. What it is for is the thing people already
-- do — most carpooling around here happens in Facebook groups, and being able
-- to look someone up where you already know them is worth more than a badge
-- that claims a check nobody performed.
--
-- The host allowlist is the safety rule. A free text field rendered as a
-- tappable link, in an app that introduces strangers, is a phishing surface;
-- restricting it to the handful of places people actually mean keeps it a
-- profile link rather than an arbitrary redirect.
ALTER TABLE users ADD COLUMN social_url text
    CONSTRAINT users_social_url_allowed CHECK (
        social_url IS NULL OR
        social_url ~ '^https://(www\.|m\.|web\.)?(facebook\.com|instagram\.com)/[A-Za-z0-9._/?=&%-]{1,180}$'
    );

-- +goose Down

ALTER TABLE users DROP COLUMN social_url;

-- +goose Up
-- Social links become handles rather than a pasted URL.
--
-- A URL field asks a person to produce something they do not have to hand. They
-- know they are @something on Instagram; they do not know which of the four
-- shapes of Facebook URL is theirs, and the one row in the development database
-- proves the point: it holds https://www.facebook.com/notifications/, which is
-- not a profile at all.
--
-- The rules below are deliberately ours rather than the platforms'. Facebook and
-- Instagram change their own username rules and their reserved names, and a
-- handle that satisfies today's rules may still be nobody. So this only checks
-- that the value is a single safe path segment, and nothing here claims the
-- account exists or belongs to the person who typed it.
--
-- social_url is kept. Dropping it in the same migration that fills the new
-- columns would destroy the only copy of anything the backfill could not parse,
-- and a down migration cannot invent those URLs back. It goes in a later
-- migration, once nothing unconverted remains.

ALTER TABLE users
  ADD COLUMN facebook_handle  text,
  ADD COLUMN instagram_handle text;

-- Backfilled only where extraction is unambiguous: exactly one path segment,
-- no query, no fragment, and not one of the platform's own pages. A share link,
-- a profile.php?id=, a post, a group or a reel is a URL that happens to contain
-- a name, and guessing at those writes somebody else's identity into a profile.
WITH parsed AS (
  SELECT id,
         lower((regexp_match(social_url,
           '^https://(?:www\.|m\.|web\.)?(facebook|instagram)\.com/([A-Za-z0-9._-]{1,60})/?$'))[1]) AS platform,
         (regexp_match(social_url,
           '^https://(?:www\.|m\.|web\.)?(facebook|instagram)\.com/([A-Za-z0-9._-]{1,60})/?$'))[2] AS handle
    FROM users
   WHERE social_url IS NOT NULL
)
UPDATE users u
   SET facebook_handle  = CASE WHEN p.platform = 'facebook'  THEN p.handle END,
       instagram_handle = CASE WHEN p.platform = 'instagram' THEN p.handle END
  FROM parsed p
 WHERE u.id = p.id
   AND p.handle IS NOT NULL
   -- The platforms' own pages, which are never somebody's profile.
   AND lower(p.handle) NOT IN (
     'notifications', 'settings', 'profile.php', 'groups', 'events', 'pages',
     'sharer', 'share', 'login', 'help', 'explore', 'reels', 'stories', 'p', 'accounts'
   );

-- +goose StatementBegin
DO $$
DECLARE unconverted int;
BEGIN
  SELECT count(*) INTO unconverted
    FROM users
   WHERE social_url IS NOT NULL
     AND facebook_handle IS NULL
     AND instagram_handle IS NULL;
  IF unconverted > 0 THEN
    -- A notice rather than an exception: these rows are not lost, social_url
    -- still holds them, and the app reads the new columns only. Blocking the
    -- migration would make one unparseable link stop the whole schema.
    RAISE NOTICE '% social_url value(s) could not be read as a handle; social_url is kept for them', unconverted;
  END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE users
  ADD CONSTRAINT users_facebook_handle_shape
    CHECK (facebook_handle IS NULL OR facebook_handle ~ '^[A-Za-z0-9._-]{1,60}$') NOT VALID,
  ADD CONSTRAINT users_instagram_handle_shape
    CHECK (instagram_handle IS NULL OR instagram_handle ~ '^[A-Za-z0-9._-]{1,60}$') NOT VALID;

ALTER TABLE users VALIDATE CONSTRAINT users_facebook_handle_shape;
ALTER TABLE users VALIDATE CONSTRAINT users_instagram_handle_shape;

-- +goose Down
ALTER TABLE users
  DROP CONSTRAINT users_facebook_handle_shape,
  DROP CONSTRAINT users_instagram_handle_shape,
  DROP COLUMN facebook_handle,
  DROP COLUMN instagram_handle;

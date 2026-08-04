-- +goose Up
-- Profile photos, stored in the database because there is nowhere else.
--
-- No object store is in the stack and none is being added for a pilot, so the
-- bytes live here. At a hundred users and a quarter-megabyte canonical image
-- that is tens of megabytes, which the existing backup already handles.
--
-- What that costs, stated so it is not discovered later: every replacement
-- writes another full image into the WAL, backups and any replica carry the
-- media, and replaced rows leave dead TOAST tuples until vacuum catches up. The
-- point to move to a bucket is when photos start deciding the restore time or
-- the disk budget, not at any particular user count.
--
-- Its own table, one row per user, so that reading a profile does not drag an
-- image through memory, and so the photo can be dropped without touching the
-- account.

CREATE TABLE profile_photos (
    user_id      uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    -- Canonical bytes: what the server re-encoded, never what was uploaded.
    data         bytea NOT NULL,
    content_type text  NOT NULL,
    -- Of the canonical bytes. Serves as the ETag and as the cache-busting part
    -- of the URL, so a replaced photo cannot be served from an old cache.
    sha256       bytea NOT NULL,
    width        integer NOT NULL,
    height       integer NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT profile_photos_type  CHECK (content_type IN ('image/jpeg', 'image/png')),
    CONSTRAINT profile_photos_hash  CHECK (octet_length(sha256) = 32),
    -- The server re-encodes to well under this. The constraint is the backstop
    -- for anything that reaches the table by another route.
    CONSTRAINT profile_photos_size  CHECK (octet_length(data) BETWEEN 1 AND 1048576),
    CONSTRAINT profile_photos_shape CHECK (width BETWEEN 1 AND 2048 AND height BETWEEN 1 AND 2048)
);

-- photo_url stops being something a user can set. It used to be an ordinary
-- field on PATCH /me, which meant anybody could point their avatar at any URL
-- on the internet: an origin they control, serving whatever they like, fetched
-- by every other user's device, and reporting back who looked.
--
-- Existing values are not imported. Fetching a URL a user supplied, from the
-- server, is exactly the request forgery this is closing. The two rows that
-- have one keep it until those people upload a photo.
COMMENT ON COLUMN users.photo_url IS
  'Legacy. Set before uploads existed; no longer writable through the API. Prefer the profile_photos table.';

-- +goose Down
DROP TABLE profile_photos;

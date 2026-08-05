-- +goose Up

-- The half-step between proving a phone number and having an account.
--
-- Verifying a code used to demand a name and a home region in the same request,
-- which meant a client had to collect them before it could sign anybody in —
-- including the people who signed up last month. Returning users were walked
-- through onboarding on every login.
--
-- Doing it the other way round runs into the code being single-use: the client
-- cannot know whether the number is new until the code is checked, and checking
-- it spends it. So verifying an unknown number hands back one of these instead
-- of a session, and the profile screen exchanges it.
--
-- A ticket is a credential, so what is stored is a digest. A database leak must
-- not hand out live signups any more than it hands out live logins, which is
-- the same reason otp_codes and refresh_tokens hold digests rather than values.
CREATE TABLE signup_tickets (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash  text NOT NULL UNIQUE,
    phone       text NOT NULL,
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT signup_tickets_phone_e164 CHECK (phone ~ '^\+[1-9][0-9]{7,14}$')
);

-- Asking for a fresh code voids any ticket outstanding for that number, which
-- is a delete by phone.
CREATE INDEX signup_tickets_phone ON signup_tickets (phone);

-- The janitor sweeps spent and expired tickets; without this it is a sequential
-- scan of every signup the app has ever seen.
CREATE INDEX signup_tickets_expires ON signup_tickets (expires_at);

-- +goose Down

DROP TABLE IF EXISTS signup_tickets;

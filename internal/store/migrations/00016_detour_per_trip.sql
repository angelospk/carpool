-- +goose Up
-- What a driver will go out of their way belongs to the trip, not to them.
--
-- It lived on driver_profiles, which said "this is how I drive, always". What a
-- driver actually means is "today I am going to Thessaloniki and today I have
-- time"; the same person on a Monday commute has none. So the profile keeps
-- defaults for filling in a new route, and the answer that binds is on the
-- route, snapshotted onto each occurrence with seats and pricing.
--
-- Three columns rather than one:
--
-- allow_stops is not the same as a budget of zero. Zero means "only if it costs
-- me nothing", and nothing is never quite true — pulling over and pulling out
-- again is time whatever the map says. False means no.
--
-- detour_km is the figure a driver actually chooses, because it is the one they
-- can picture.
--
-- detour_min is there because the two do not track each other. Fifteen
-- kilometres on the Egnatia is nine minutes; fifteen up a mountain road is
-- thirty-five. Somebody who agreed to fifteen kilometres did not agree to
-- thirty-five minutes, and only the clock can say so.

ALTER TABLE driver_profiles
  ADD COLUMN default_allow_stops boolean NOT NULL DEFAULT true,
  ADD COLUMN default_detour_km numeric(4,1) NOT NULL DEFAULT 10.0
      CHECK (default_detour_km >= 0 AND default_detour_km <= 50);

ALTER TABLE route_templates
  ADD COLUMN allow_stops boolean NOT NULL DEFAULT true,
  ADD COLUMN detour_km numeric(4,1) NOT NULL DEFAULT 10.0
      CHECK (detour_km >= 0 AND detour_km <= 50),
  -- Mirrors driver_profiles.detour_budget_min, which stays as the default.
  ADD COLUMN detour_min integer NOT NULL DEFAULT 10
      CHECK (detour_min >= 0 AND detour_min <= 30);

-- The same bounds as the template they are copied from. A snapshot that can
-- hold a value its source could not is a snapshot that stops being a copy the
-- first time anything writes to it directly.
ALTER TABLE trip_occurrences
  ADD COLUMN allow_stops boolean NOT NULL DEFAULT true,
  ADD COLUMN detour_km numeric(4,1) NOT NULL DEFAULT 10.0
      CHECK (detour_km >= 0 AND detour_km <= 50),
  ADD COLUMN detour_min integer NOT NULL DEFAULT 10
      CHECK (detour_min >= 0 AND detour_min <= 30);

-- A driver who had set a budget of zero had said, in the only way the old
-- schema allowed, that they wanted no detours. The column default of true would
-- quietly reverse that, so their default comes from what they had chosen rather
-- than from what is convenient for new accounts.
UPDATE driver_profiles
   SET default_allow_stops = detour_budget_min > 0,
       default_detour_km = CASE WHEN detour_budget_min > 0 THEN 10.0 ELSE 0.0 END;

-- Existing routes inherit what their driver had been driving under, so nobody's
-- published trip silently changes terms. The default above only covers rows
-- created after this runs.
UPDATE route_templates t
   SET detour_min = d.detour_budget_min,
       detour_km = d.default_detour_km,
       allow_stops = d.default_allow_stops
  FROM driver_profiles d
 WHERE d.user_id = t.driver_id;

UPDATE trip_occurrences o
   SET detour_min = t.detour_min,
       detour_km = t.detour_km,
       allow_stops = t.allow_stops
  FROM route_templates t
 WHERE t.id = o.template_id;

-- +goose Down
ALTER TABLE trip_occurrences
  DROP COLUMN allow_stops, DROP COLUMN detour_km, DROP COLUMN detour_min;
ALTER TABLE route_templates
  DROP COLUMN allow_stops, DROP COLUMN detour_km, DROP COLUMN detour_min;
ALTER TABLE driver_profiles
  DROP COLUMN default_allow_stops, DROP COLUMN default_detour_km;

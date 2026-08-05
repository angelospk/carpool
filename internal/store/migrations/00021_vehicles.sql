-- +goose Up

-- A driver has one car, until they have two.
--
-- driver_profiles held the car inline, one row per person, so a driver with a
-- van for the weekend and a small diesel for the week had to edit their profile
-- before every trip — and every trip already published silently re-priced when
-- they did, because pricing read the profile as it stood rather than as it was.
--
-- So the car becomes a thing of its own, and a trip names which one.
CREATE TABLE vehicles (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Everything here is copied off the άδεια κυκλοφορίας. None of it is the
    -- driver's judgement, which is the point: the consumption below is derived
    -- from it, never typed.
    make       text NOT NULL,
    model      text NOT NULL,
    fuel       text NOT NULL
               CHECK (fuel IN ('petrol', 'diesel', 'petrol_hybrid', 'diesel_hybrid')),
    engine_cc  int  CHECK (engine_cc IS NULL OR engine_cc BETWEEN 500 AND 8000),
    co2_g_km   int  CHECK (co2_g_km IS NULL OR co2_g_km BETWEEN 20 AND 600),

    consumption_l_100km numeric(4,1) NOT NULL
                        CHECK (consumption_l_100km > 0 AND consumption_l_100km < 100),
    consumption_source  text NOT NULL
                        CHECK (consumption_source IN ('legacy', 'dataset', 'co2', 'band')),
    assessed_at         timestamptz NOT NULL DEFAULT now(),

    -- What the driver calls it, so that two Yarises are tellable apart in a
    -- list. Optional; the make and model are the fallback.
    label      text CHECK (label IS NULL OR length(label) BETWEEN 1 AND 40),
    colour     text,
    -- Revealed to a passenger only after their seat is accepted.
    plate      text NOT NULL,

    is_default boolean NOT NULL DEFAULT false,
    -- Retired rather than deleted: a template may still point at it, and a
    -- passenger asking what they rode in last March deserves an answer.
    retired_at timestamptz,

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- At most one default among the cars a driver still has.
CREATE UNIQUE INDEX vehicles_one_default ON vehicles (owner_id)
    WHERE is_default AND retired_at IS NULL;
CREATE INDEX vehicles_owner_live ON vehicles (owner_id) WHERE retired_at IS NULL;

-- The target of the composite foreign key below, which is what makes
-- "you may only publish with your own car" a rule the database keeps rather
-- than one a handler remembers.
CREATE UNIQUE INDEX vehicles_id_owner ON vehicles (id, owner_id);

-- +goose StatementBegin
DO $$
BEGIN
    -- One vehicle per existing profile, carrying its figures across unchanged.
    -- Re-assessing here would reprice trips that are already published, on the
    -- day of a migration, for reasons nobody could see.
    INSERT INTO vehicles (owner_id, make, model, fuel, engine_cc, co2_g_km,
                          consumption_l_100km, consumption_source, assessed_at,
                          colour, plate, is_default)
    SELECT d.user_id,
           coalesce(d.vehicle_make, d.car_model, 'unknown'),
           coalesce(d.vehicle_model, d.car_model, 'unknown'),
           coalesce(d.vehicle_fuel, 'petrol'),
           d.engine_cc, d.co2_g_km,
           d.fuel_consumption_l_100km, d.consumption_source,
           coalesce(d.assessed_at, d.created_at),
           d.car_color, d.car_plate,
           true
      FROM driver_profiles d;
END
$$;
-- +goose StatementEnd

-- Which car a route is published with, and which car a given departure actually
-- goes out in.
--
-- Both, deliberately. The template's is the default for departures generated
-- from it; the occurrence's is the one that was chosen for that morning. If
-- only the template carried it, editing a route would silently change the car
-- named on trips already booked — the make and colour a passenger is standing
-- at the kerb looking for.
ALTER TABLE route_templates  ADD COLUMN vehicle_id uuid;
ALTER TABLE trip_occurrences ADD COLUMN vehicle_id uuid;

UPDATE route_templates t SET vehicle_id = v.id
  FROM vehicles v WHERE v.owner_id = t.driver_id AND v.is_default;

UPDATE trip_occurrences o SET vehicle_id = t.vehicle_id
  FROM route_templates t WHERE t.id = o.template_id;

-- Not null once the backfill has run, so a code path that forgets to choose a
-- car fails at the insert rather than by pricing the trip off nothing.
ALTER TABLE route_templates  ALTER COLUMN vehicle_id SET NOT NULL;
ALTER TABLE trip_occurrences ALTER COLUMN vehicle_id SET NOT NULL;

-- Your own car, enforced by the database. A handler that forgets to check
-- cannot publish a route with somebody else's Yaris on it.
ALTER TABLE route_templates
    ADD CONSTRAINT route_templates_vehicle_is_the_drivers
    FOREIGN KEY (vehicle_id, driver_id) REFERENCES vehicles (id, owner_id);

ALTER TABLE trip_occurrences
    ADD CONSTRAINT trip_occurrences_vehicle FOREIGN KEY (vehicle_id) REFERENCES vehicles (id);

CREATE INDEX route_templates_vehicle  ON route_templates (vehicle_id);
CREATE INDEX trip_occurrences_vehicle ON trip_occurrences (vehicle_id);

-- The car columns on driver_profiles are not dropped here. Every reader has to
-- move first, and a column still present is a rollback while a column dropped
-- is a restore. What stays on the profile is the part that is about the person
-- rather than the car: how far out of their way they will go, how near they
-- want to be notified about, what they will not charge for.

-- +goose Down

ALTER TABLE trip_occurrences DROP CONSTRAINT IF EXISTS trip_occurrences_vehicle;
ALTER TABLE route_templates  DROP CONSTRAINT IF EXISTS route_templates_vehicle_is_the_drivers;
ALTER TABLE trip_occurrences DROP COLUMN IF EXISTS vehicle_id;
ALTER TABLE route_templates  DROP COLUMN IF EXISTS vehicle_id;
DROP TABLE IF EXISTS vehicles;

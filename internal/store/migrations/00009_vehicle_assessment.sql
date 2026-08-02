-- +goose Up

-- What the car is, so the app can work out what it drinks instead of asking.
--
-- fuel_consumption_l_100km stays, and stays the column pricing reads, but it
-- stops being an input. It becomes the answer internal/vehicles arrived at from
-- the columns added here, written by the server, never by the client.
--
-- Keeping the number denormalised rather than looking it up at price time is
-- deliberate. A dataset refresh moves a model's figure, and a trip already
-- published must not silently reprice because a CSV changed underneath it.
ALTER TABLE driver_profiles
    ADD COLUMN vehicle_make  text,
    ADD COLUMN vehicle_model text,
    ADD COLUMN vehicle_fuel  text
        CHECK (vehicle_fuel IN ('petrol','diesel','petrol_hybrid','diesel_hybrid')),
    ADD COLUMN engine_cc     int  CHECK (engine_cc IS NULL OR engine_cc BETWEEN 500 AND 8000),
    -- Field V.7 of the άδεια κυκλοφορίας. Null for cars first registered before
    -- 1 November 2010, whose documents do not carry it.
    ADD COLUMN co2_g_km      int  CHECK (co2_g_km IS NULL OR co2_g_km BETWEEN 20 AND 600),
    -- Which of the three sources the figure came from. A booking priced off a
    -- 'band' figure was priced off a floor for the whole class, and somebody
    -- reading it back a year later needs to know that.
    ADD COLUMN consumption_source text NOT NULL DEFAULT 'legacy'
        CHECK (consumption_source IN ('legacy','dataset','co2','band')),
    ADD COLUMN assessed_at timestamptz;

-- Every profile that exists today was created by hand and carries the old flat
-- 7.0 default. A small petrol car drinks about 5.6, so those rows have been
-- quoting a quarter over what they burnt. They are marked rather than guessed
-- at: 'legacy' is not one of the three sources, so anything that asks "is this
-- verified" gets the right answer, and re-assessing them is a person's job.
--
-- No UPDATE of the number itself. Changing a price without changing what the
-- driver told us would be inventing data.

-- +goose Down

ALTER TABLE driver_profiles
    DROP COLUMN vehicle_make,
    DROP COLUMN vehicle_model,
    DROP COLUMN vehicle_fuel,
    DROP COLUMN engine_cc,
    DROP COLUMN co2_g_km,
    DROP COLUMN consumption_source,
    DROP COLUMN assessed_at;

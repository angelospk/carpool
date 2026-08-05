-- +goose Up

-- One fuel price per region was one number too few, in two different ways.
--
-- It could not tell petrol from diesel. Every trip in the country was priced at
-- the same €/litre whatever the car burnt, and the two differ by ten cents or
-- more at the pump, so a diesel was quoted as though it drank petrol or the
-- other way round — on top of a consumption figure that is already
-- fuel-specific. That is the correctness bug here.
--
-- And it sat on the region, which invited seventy-five copies of one national
-- figure. Seventy-five rows all reading 1.800 look like regional prices and are
-- not; anybody comparing Ιωάννινα with Ρόδο would be reading a fiction. The
-- Παρατηρητήριο Τιμών does publish by νομό, but nothing here fetches it, and
-- inventing seventy-five numbers to look thorough is worse than admitting to
-- one.
--
-- So: a national row per fuel, and room for a regional override where somebody
-- has actually gone and looked one up. NULL region means national, and the
-- lookup prefers the specific.
CREATE TABLE fuel_prices (
    region_id  uuid REFERENCES regions(id) ON DELETE CASCADE,
    -- Only the two liquids. A hybrid burns petrol or diesel at the same price
    -- per litre as anything else; that it burns less of it is what the
    -- consumption figure says, and saying it twice would be saying it wrong.
    fuel       text NOT NULL CHECK (fuel IN ('petrol', 'diesel')),
    price_eur  numeric(4,3) NOT NULL CHECK (price_eur > 0),
    -- Where the figure came from, in words, because a price nobody can trace is
    -- a price nobody can correct.
    source     text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- One national figure per fuel, and one override per region per fuel. Two
-- partial indexes rather than one over a nullable column, because NULLs do not
-- collide in a unique index and the national rows would be free to duplicate.
CREATE UNIQUE INDEX fuel_prices_national ON fuel_prices (fuel) WHERE region_id IS NULL;
CREATE UNIQUE INDEX fuel_prices_regional ON fuel_prices (region_id, fuel) WHERE region_id IS NOT NULL;

-- +goose StatementBegin
DO $$
DECLARE
    pilot numeric(4,3);
BEGIN
    -- Whatever the pilot was seeded with becomes both national figures, so no
    -- price moves on the day of this migration. It is the same number for both
    -- fuels, which is exactly as wrong as it was yesterday and now visible as
    -- such: the source says so, and updated_at will show it has not been
    -- touched since.
    SELECT max(fuel_price_eur) INTO pilot FROM regions;
    IF pilot IS NOT NULL THEN
        INSERT INTO fuel_prices (region_id, fuel, price_eur, source)
        VALUES (NULL, 'petrol', pilot, 'carried over from the single pilot figure'),
               (NULL, 'diesel', pilot, 'carried over from the single pilot figure');
    END IF;
END
$$;
-- +goose StatementEnd

-- The column it replaces. Left in place for now rather than dropped in the same
-- migration: every reader has to move first, and a column that is still there
-- is a rollback, while a column that is gone is a restore.
--
-- It gets a default so that seeding a region no longer has to invent a number
-- for it. Nothing reads it from here on.
ALTER TABLE regions ALTER COLUMN fuel_price_eur SET DEFAULT 1.0;

COMMENT ON COLUMN regions.fuel_price_eur IS
    'Superseded by fuel_prices. Read nothing from this; it is kept only so 00019 can be rolled back.';

-- +goose Down

DROP TABLE IF EXISTS fuel_prices;
ALTER TABLE regions ALTER COLUMN fuel_price_eur DROP DEFAULT;
COMMENT ON COLUMN regions.fuel_price_eur IS NULL;

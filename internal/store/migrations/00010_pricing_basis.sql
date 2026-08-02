-- +goose Up

-- Why a price was that price, frozen alongside it.
--
-- A booking already stores price_eur. It does not store how that number was
-- reached, and every input lives somewhere that changes: the driver swaps cars,
-- the vehicle dataset is refreshed, the region's fuel price is edited, the trip
-- is re-routed. So "why did I pay 3.50 € in March" is currently unanswerable,
-- and the honest answer a year from now would be a reconstruction rather than a
-- record.
--
-- One jsonb column rather than eight typed ones, deliberately. This is a frozen
-- receipt: nothing joins on it, it is read back whole to explain one row, and
-- adding a field later must not mean a migration that leaves historical rows
-- with a column they could never have filled. jsonb still answers "which
-- bookings were priced off a class floor" via ->>.
--
-- It goes on all three tables because a booking can arrive by three routes: an
-- ask that was accepted, a standing arrangement, or a week generated from that
-- arrangement. The basis has to survive each hop or the chain breaks at
-- whichever one forgot.
ALTER TABLE asks                  ADD COLUMN pricing_basis jsonb;
ALTER TABLE standing_arrangements ADD COLUMN pricing_basis jsonb;
ALTER TABLE bookings              ADD COLUMN pricing_basis jsonb;

-- Nullable, and left null on the rows that already exist. There is no way to
-- work out what those prices were made of, and filling them with today's inputs
-- would turn a gap into a plausible fiction.

CREATE INDEX bookings_pricing_source ON bookings ((pricing_basis ->> 'consumption_source'))
    WHERE pricing_basis IS NOT NULL;

-- When the region's fuel price was last touched.
--
-- It is one number per region, edited by hand, and when it goes stale it is
-- wrong for everybody in that region at once. Nothing currently shows that. A
-- timestamp does not fix the staleness, but it makes it visible, which is the
-- difference between a known limitation and a silent one.
ALTER TABLE regions ADD COLUMN fuel_price_updated_at timestamptz NOT NULL DEFAULT now();

-- +goose Down

DROP INDEX bookings_pricing_source;
ALTER TABLE asks                  DROP COLUMN pricing_basis;
ALTER TABLE standing_arrangements DROP COLUMN pricing_basis;
ALTER TABLE bookings              DROP COLUMN pricing_basis;
ALTER TABLE regions               DROP COLUMN fuel_price_updated_at;

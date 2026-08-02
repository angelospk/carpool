-- +goose Up

-- Pin wear_factor to 1.0 for the pilot.
--
-- The column let a driver multiply the whole fuel cost by up to 1.5, which is
-- to say ask for 50% more than the trip burned, on the grounds that a car wears
-- out as well as drinking. That is true. It is not what this multiplier
-- measures: wear is roughly a fixed amount per kilometre, so tying it to the
-- fuel bill makes it swing with the price of diesel and with how thirsty the
-- car is, neither of which has anything to do with tyres.
--
-- It also sat on the same formula as the driver-typed consumption, aimed the
-- same way. Άρθρο 18 ν. 1903/1990, as amended by άρθρο 36 ν. 4663/2020, fines a
-- private car carrying passengers με κόμιστρο, and what the driver has to argue
-- is that they did not come out ahead. A 1.5 multiplier is the driver putting
-- 50% above cost into their own hands and calling it wear.
--
-- Nothing in the API ever wrote this column, so every existing row is already
-- at the 1.0 default and the UPDATE below is a formality. That is exactly why
-- it is worth constraining now rather than later: the constraint is free today
-- and becomes a data migration the moment somebody adds the handler.
--
-- Reinstating it means a new migration and, per the reasoning above, a
-- different shape: an additive euro-per-kilometre rate the app sets, not a
-- multiplier the driver sets.
UPDATE driver_profiles SET wear_factor = 1.0 WHERE wear_factor <> 1.0;

ALTER TABLE driver_profiles DROP CONSTRAINT driver_profiles_wear_factor_check;
ALTER TABLE driver_profiles ADD CONSTRAINT driver_profiles_wear_factor_check
    CHECK (wear_factor = 1.0);

-- +goose Down

-- Reversible, unlike the migrations that drop data: this one only narrows a
-- constraint, and widening it back loses nothing, because every row is at 1.0
-- either way.
ALTER TABLE driver_profiles DROP CONSTRAINT driver_profiles_wear_factor_check;
ALTER TABLE driver_profiles ADD CONSTRAINT driver_profiles_wear_factor_check
    CHECK (wear_factor BETWEEN 1.0 AND 1.5);

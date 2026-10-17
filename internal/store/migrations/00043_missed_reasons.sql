-- +goose Up
-- More reasons why nobody was asked although a car passes (QA 2026-10-17):
-- it is full (full), it already carries the most groups it takes (party_cap),
-- the luggage does not fit (luggage), or its road passes within 10 km of the
-- passenger but outside the distance its driver stops at (off_corridor, with
-- need the distance and limit that radius, both in km).
ALTER TABLE ride_requests DROP CONSTRAINT ride_requests_missed_reason_check;
ALTER TABLE ride_requests ADD CONSTRAINT ride_requests_missed_reason_check
    CHECK (missed_reason IN ('no_stops', 'detour_min', 'detour_km',
                             'full', 'party_cap', 'luggage', 'off_corridor'));

-- +goose Down
UPDATE ride_requests SET missed_reason = NULL, missed_need = NULL, missed_limit = NULL
 WHERE missed_reason IN ('full', 'party_cap', 'luggage', 'off_corridor');
ALTER TABLE ride_requests DROP CONSTRAINT ride_requests_missed_reason_check;
ALTER TABLE ride_requests ADD CONSTRAINT ride_requests_missed_reason_check
    CHECK (missed_reason IN ('no_stops', 'detour_min', 'detour_km'));

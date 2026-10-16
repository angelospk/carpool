-- +goose Up
-- One journey, several days it could happen on: "Thursday, or else Friday or
-- Saturday". Each day is its own request, matched on its own; alternative_of
-- points every extra day at the first one. When a driver is accepted for any
-- of them the others are cancelled, so the passenger is not carried twice.
ALTER TABLE ride_requests
    ADD COLUMN alternative_of uuid NULL REFERENCES ride_requests(id) ON DELETE SET NULL;

CREATE INDEX ride_requests_alternative_of ON ride_requests (alternative_of)
    WHERE alternative_of IS NOT NULL;

-- +goose Down
DROP INDEX ride_requests_alternative_of;
ALTER TABLE ride_requests DROP COLUMN alternative_of;

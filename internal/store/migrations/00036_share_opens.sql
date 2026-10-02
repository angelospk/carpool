-- +goose Up
-- How many times a person opened a route's link. A count and nothing else:
-- no address, no time, no device, so it says whether the share travels
-- without saying who followed it. Only the route's driver sees it.
ALTER TABLE route_templates ADD COLUMN share_opens int NOT NULL DEFAULT 0 CHECK (share_opens >= 0);

-- +goose Down
ALTER TABLE route_templates DROP COLUMN share_opens;

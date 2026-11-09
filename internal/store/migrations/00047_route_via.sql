-- +goose Up
-- A point on the road the driver picked when it was not the fastest one. The
-- matcher routes the driver's own journey through it, so a passenger on that
-- road is not charged as a detour from a road the driver never meant to take.
ALTER TABLE route_templates ADD COLUMN via geometry(Point, 4326)
    CHECK (via IS NULL OR ST_SRID(via) = 4326);

-- +goose Down
ALTER TABLE route_templates DROP COLUMN via;

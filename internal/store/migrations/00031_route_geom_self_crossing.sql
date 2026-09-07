-- +goose Up

-- Real roads cross themselves, so route_templates cannot demand otherwise.
--
-- route_templates_geom_simple rejected any route_geom failing ST_IsSimple. The
-- reasoning in 00001 was that ST_LineLocatePoint cannot establish a unique
-- traversal position on a self-intersecting line and the gross match depends on
-- it. The premise is right; the conclusion refused ordinary roads.
--
-- Measured against the Greece extract on 2 September 2026: of ten routes
-- between 2.5 and 164 km, Αλεξανδρούπολη → Μάκρη came back non-simple — 516
-- vertices, not one of them repeated, a single genuine crossing at
-- (25.75149, 40.85981). ST_RemoveRepeatedPoints changes nothing. It is not a
-- degenerate shape to be cleaned up before storing; it is what the road does,
-- and RISKS.md item 14 recorded the same thing a month earlier for the
-- Ορεστιάδα → Αλεξανδρούπολη direction of the corridor this pilot exists to
-- serve. A driver could publish their morning commute and not the evening
-- return, and the only thing on screen was "the route was rejected".
--
-- Repairing the geometry instead was considered and rejected. ST_Node,
-- ST_UnaryUnion and ST_LineMerge work on planar linework rather than on an
-- ordered traversal: they can drop a repeated pass, reverse a direction, or
-- hand back a MultiLineString. The stored line has to stay exactly the ordered
-- sequence OSRM returned, because that order is what every fraction along it
-- means.
--
-- What is accepted with this: on a non-simple line ST_LineLocatePoint returns
-- the earliest position of minimum distance rather than failing, so a pin
-- sitting within metres of the crossing can be placed on the first pass instead
-- of the second. On the route above the two passes are 0.54 km apart along a
-- 17.8 km line. The bound is the route length, not that gap, so the debt is
-- real and is written down in RISKS.md item 14 rather than papered over here.
-- The alternative was a matcher that publishes nothing.
--
-- ST_NPoints >= 2 and the SRID check stay: those reject geometry that is not a
-- route at all, which is a different claim.
--
-- Takes ACCESS EXCLUSIVE on route_templates for the length of a catalogue
-- update. No rows are read or rewritten, so it is over in milliseconds even
-- once the table is large.
ALTER TABLE route_templates DROP CONSTRAINT route_templates_geom_simple;

-- +goose Down

-- NOT VALID, deliberately. By the time anyone rolls this back the table holds
-- the self-crossing routes the Up migration exists to admit, and a validated
-- constraint would fail against them — leaving the choice between a rollback
-- that cannot run and one that deletes somebody's published route. Neither is
-- a rollback. New writes are checked again; the rows already there are not.
ALTER TABLE route_templates
    ADD CONSTRAINT route_templates_geom_simple CHECK (ST_IsSimple(route_geom)) NOT VALID;

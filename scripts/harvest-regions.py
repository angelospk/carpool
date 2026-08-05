#!/usr/bin/env python3
"""Build the region fixture: every περιφερειακή ενότητα of Greece.

A region is what the app prices a trip against — one fuel price per boundary —
and until now there was exactly one of them, Έβρος, because the pilot was one
prefecture. Opening the app to the whole country left every trip in the country
priced off Έβρος' fuel, which is wrong twice over: wrong for the driver in
Ιωάννινα, and wrong for the driver from Έβρος driving Αθήνα → Πάτρα.

The geography is the περιφερειακή ενότητα (74 of them, the old νομοί plus the
Attica split) rather than the περιφέρεια (13). That is the level the
Παρατηρητήριο Τιμών Υγρών Καυσίμων publishes fuel prices at, so it is the level
at which a price can be corrected by hand later without redrawing anything.

In OSM these sit at admin_level=6 and are named in the genitive: "Περιφερειακή
Ενότητα Έβρου", plus the one that calls itself "Μητροπολιτική Ενότητα
Θεσσαλονίκης". Both prefixes are matched explicitly rather than filtering
level 6 by country, because level 6 in the same extract also holds Albanian
qarqe, and a name prefix is a check somebody can read.

Usage:

    osmium tags-filter osrm-data/greece-latest.osm.pbf r/boundary=administrative \\
        -o /tmp/admin.osm.pbf
    osmium export -f geojsonseq --geometry-types=polygon -u type_id \\
        /tmp/admin.osm.pbf -o /tmp/admin.geojsonseq
    scripts/harvest-regions.py /tmp/admin.geojsonseq fixtures/regions/greece.geojson
"""

import json
import math
import sys

# The two prefixes Greek περιφερειακές ενότητες carry in OSM.
PREFIXES = ("Περιφερειακή Ενότητα ", "Μητροπολιτική Ενότητα ")

# Άγιο Όρος is in no περιφερειακή ενότητα. It is an autonomous monastic polity
# and OSM puts it at admin_level=3, alongside nothing else in the country, so
# filtering by level 6 leaves a 330 km² hole in the middle of Χαλκιδική's
# peninsula.
#
# It is taken in as a region of its own rather than folded into Χαλκιδική. That
# keeps the country watertight, which is what the "no region covers this point"
# path is allowed to mean, and it does not assert that Δάφνη buys its diesel in
# Πολύγυρος.
EXTRA = {"Αυτόνομη Μοναστική Πολιτεία Αγίου Όρους": "3"}

# Simplification tolerance in degrees. 0.002° is roughly 170 m of latitude,
# which is far finer than the question being asked of these polygons — "which
# prefecture is this town in" — and takes the fixture from tens of megabytes to
# something a repository can hold.
#
# Rings are simplified independently, so a shared border can open a sliver
# where two neighbours no longer meet exactly. A point landing in such a sliver
# belongs to no region, which is why the lookup falls back to the nearest one
# rather than failing. That fallback is needed regardless: for a pickup at a
# port whose pin sits in the water, and for Άγιο Όρος, which is in no
# περιφερειακή ενότητα at all.
TOLERANCE = 0.002

# What a region was called by an earlier fixture, so the seeder adopts the row
# that already exists rather than inserting a second one for the same ground.
# The pilot ran in one prefecture and called it by its nominative short name.
PREVIOUS_NAMES = {
    "Περιφερειακή Ενότητα Έβρου": "Έβρος",
}

# A ring below this many points is left alone. Small islands are all coastline
# and simplifying them further turns them into triangles.
MIN_RING = 12


def perpendicular_distance(p, a, b):
    """Distance from p to the segment ab, in degrees.

    Longitude is scaled by cos(latitude) so that the tolerance means roughly
    the same distance on the ground in Κρήτη as in Έβρος. Greece spans 35°–42°,
    where the factor runs from 0.82 to 0.74, so ignoring it would make the
    tolerance a quarter tighter in the north than in the south.
    """
    scale = math.cos(math.radians(a[1]))
    ax, ay = a[0] * scale, a[1]
    bx, by = b[0] * scale, b[1]
    px, py = p[0] * scale, p[1]

    dx, dy = bx - ax, by - ay
    if dx == 0 and dy == 0:
        return math.hypot(px - ax, py - ay)

    # Projection of ap onto ab, clamped to the segment.
    t = ((px - ax) * dx + (py - ay) * dy) / (dx * dx + dy * dy)
    t = max(0.0, min(1.0, t))
    return math.hypot(px - (ax + t * dx), py - (ay + t * dy))


def simplify(points, tolerance):
    """Ramer-Douglas-Peucker, iterative so a 200k-point coastline cannot blow
    the recursion limit."""
    if len(points) < 3:
        return list(points)

    keep = [False] * len(points)
    keep[0] = keep[-1] = True
    stack = [(0, len(points) - 1)]

    while stack:
        first, last = stack.pop()
        if last <= first + 1:
            continue

        worst, worst_at = tolerance, -1
        for i in range(first + 1, last):
            d = perpendicular_distance(points[i], points[first], points[last])
            if d > worst:
                worst, worst_at = d, i

        if worst_at != -1:
            keep[worst_at] = True
            stack.append((first, worst_at))
            stack.append((worst_at, last))

    return [p for p, k in zip(points, keep) if k]


def simplify_ring(ring):
    """Simplify a closed ring, keeping it closed and keeping it a polygon.

    A ring that simplifies below four points (three corners plus the repeated
    first) is no longer an area, so it is kept as it was. That happens for the
    smallest islets, where every point is a corner.
    """
    if len(ring) < MIN_RING:
        return ring

    out = simplify(ring[:-1], TOLERANCE)
    if len(out) < 3:
        return ring
    return out + [out[0]]


def area_of(ring):
    """Twice the signed shoelace area, used only to compare sizes."""
    total = 0.0
    for (x1, y1), (x2, y2) in zip(ring, ring[1:]):
        total += x1 * y2 - x2 * y1
    return abs(total)


def short_name(name):
    """The name without its administrative prefix, for showing to a person."""
    for prefix in PREFIXES:
        if name.startswith(prefix):
            return name[len(prefix):]
    return name


def main():
    if len(sys.argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2

    source, target = sys.argv[1], sys.argv[2]

    # An ενότητα can appear more than once if OSM holds an outdated duplicate
    # relation; the larger geometry wins, and a name collision is reported
    # rather than silently resolved.
    found = {}

    with open(source, encoding="utf-8") as fh:
        for line in fh:
            line = line.strip().lstrip("\x1e")
            if not line:
                continue

            feature = json.loads(line)
            props = feature.get("properties", {})
            name = str(props.get("name", ""))

            level = str(props.get("admin_level", ""))
            wanted = (level == "6" and name.startswith(PREFIXES)) or \
                     (name in EXTRA and EXTRA[name] == level)
            if not wanted:
                continue

            geometry = feature["geometry"]
            if geometry["type"] == "Polygon":
                polygons = [geometry["coordinates"]]
            elif geometry["type"] == "MultiPolygon":
                polygons = geometry["coordinates"]
            else:
                continue

            size = sum(area_of(poly[0]) for poly in polygons)
            if name in found and found[name][0] >= size:
                print(f"  duplicate relation for {name}, keeping the larger",
                      file=sys.stderr)
                continue

            simplified = [[simplify_ring(ring) for ring in poly] for poly in polygons]

            found[name] = (size, {
                "type": "Feature",
                "properties": {
                    "name": name,
                    "previous_name": PREVIOUS_NAMES.get(name, ""),
                    "short_name": short_name(name),
                    "admin_level": int(props.get("admin_level", 6)),
                    "osm_id": feature.get("id", ""),
                    "source": "OpenStreetMap via geofabrik greece-latest, "
                              "extracted with osmium, simplified to "
                              f"{TOLERANCE}°",
                    "license": "ODbL",
                },
                "geometry": {
                    "type": "MultiPolygon",
                    "coordinates": simplified,
                },
            })

    features = [entry[1] for _, entry in sorted(found.items())]
    if not features:
        print("no περιφερειακές ενότητες found — is this the right file?",
              file=sys.stderr)
        return 1

    with open(target, "w", encoding="utf-8") as fh:
        json.dump({"type": "FeatureCollection", "features": features},
                  fh, ensure_ascii=False, separators=(",", ":"))
        fh.write("\n")

    points = sum(len(ring)
                 for f in features
                 for poly in f["geometry"]["coordinates"]
                 for ring in poly)
    print(f"{len(features)} regions, {points} points -> {target}")
    return 0


if __name__ == "__main__":
    sys.exit(main())

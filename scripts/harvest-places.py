#!/usr/bin/env python3
"""Refresh the settlement list at internal/places/data/evros.csv.

    python3 scripts/harvest-places.py            # fetch, diff, exit 1 if stale
    python3 scripts/harvest-places.py --update   # write the dataset

Why this exists: pickup points were typed as "25.8743, 40.8476 Αλεξανδρούπολη",
which is not a thing anyone will do twice. A person types a town name, so there
has to be a list of town names with coordinates attached.

The source is OpenStreetMap, queried through Overpass against the Evros
relation rather than a bounding box: a box around this prefecture reaches into
Turkey and Bulgaria, and offering a driver a pickup point across a border is
worse than offering none.

Only nodes are harvested. Overpass can return ways and relations too, but their
`center` is the middle of a bounding box and is not guaranteed to sit inside the
feature, let alone somewhere a car can stop. Settlements in OSM are nodes in
practice, and a point that is wrong by a kilometre is worse than absent.

Names are kept as OSM has them rather than romanised here: name, name:en,
int_name and alt_name each carry authority that a generated transliteration
does not, and the Go side ranks them above what it generates itself.

Output: © OpenStreetMap contributors, ODbL.
"""
import argparse
import csv
import json
import os
import pathlib
import sys
import urllib.parse
import urllib.request

RELATION = 939217  # Evros, admin_level 6
REGION = "evros"
KINDS = ("city", "town", "village")
OUT = pathlib.Path(__file__).resolve().parent.parent / "internal/places/data/evros.csv"
ENDPOINT = "https://overpass-api.de/api/interpreter"

QUERY = f"""
[out:json][timeout:180];
rel({RELATION}); map_to_area->.a;
node["place"~"^({'|'.join(KINDS)})$"]["name"](area.a);
out body;
"""

FIELDS = ["osm_type", "osm_id", "region", "kind", "name", "name_en", "int_name", "alt_name", "lon", "lat"]


def fetch() -> list[dict]:
    body = urllib.parse.urlencode({"data": QUERY}).encode()
    # Overpass answers 406 to urllib's default User-Agent. Identifying the
    # caller is also what its usage policy asks for.
    req = urllib.request.Request(
        ENDPOINT, data=body, headers={"User-Agent": "carpool-harvest/1.0 (+local pilot, Evros)"}
    )
    with urllib.request.urlopen(req, timeout=300) as r:
        return json.load(r)["elements"]


def rows(elements: list[dict]) -> list[dict]:
    out, seen = [], set()
    for e in elements:
        tags = e.get("tags", {})
        name, kind = tags.get("name"), tags.get("place")
        lon, lat = e.get("lon"), e.get("lat")

        # Every one of these has been wrong in real OSM data at some point, and
        # a settlement with no name or a coordinate in the Gulf of Guinea is
        # worse than one that is missing: it is offered to a driver as a place
        # to stop.
        if not name or kind not in KINDS:
            continue
        if lon is None or lat is None:
            continue
        if not (-180 <= lon <= 180 and -90 <= lat <= 90):
            print(f"skipping {name}: coordinates out of range", file=sys.stderr)
            continue

        key = ("node", e["id"])
        if key in seen:  # Overpass does not promise to deduplicate
            continue
        seen.add(key)

        out.append(
            {
                "osm_type": "node",
                "osm_id": e["id"],
                "region": REGION,
                "kind": kind,
                "name": name,
                "name_en": tags.get("name:en", ""),
                "int_name": tags.get("int_name", ""),
                "alt_name": tags.get("alt_name", ""),
                "lon": f"{lon:.6f}",
                "lat": f"{lat:.6f}",
            }
        )

    # Sorted by id so that two harvests of unchanged data produce byte-identical
    # files and the diff below means something.
    return sorted(out, key=lambda r: r["osm_id"])


def render(data: list[dict]) -> str:
    import io

    buf = io.StringIO()
    w = csv.DictWriter(buf, fieldnames=FIELDS, lineterminator="\n")
    w.writeheader()
    w.writerows(data)
    return buf.getvalue()


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--update", action="store_true", help="write the dataset")
    args = ap.parse_args()

    data = rows(fetch())
    if not data:
        print("the query returned nothing; refusing to write", file=sys.stderr)
        return 1

    fresh = render(data)
    current = OUT.read_text(encoding="utf-8") if OUT.exists() else ""

    if fresh == current:
        print(f"{OUT.name}: unchanged, {len(data)} places")
        return 0

    kinds = {k: sum(1 for r in data if r["kind"] == k) for k in KINDS}
    print(f"{OUT.name}: {len(data)} places ({kinds})")
    if current:
        was = {r["name"] for r in csv.DictReader(current.splitlines())}
        now = {r["name"] for r in data}
        for label, diff in (("added", now - was), ("removed", was - now)):
            if diff:
                print(f"  {label}: {', '.join(sorted(diff))}")

    if not args.update:
        print("run again with --update to write it", file=sys.stderr)
        return 1

    OUT.parent.mkdir(parents=True, exist_ok=True)
    # Encoding stated rather than inherited: every name in here is Greek, and on
    # a machine whose locale is not UTF-8 the default would write mojibake into
    # a file that is then embedded in the binary and shipped.
    #
    # The temporary name carries the pid so two runs cannot write through each
    # other, and is removed even when the replace fails, rather than left behind
    # to be mistaken for the dataset.
    tmp = OUT.with_suffix(f".part.{os.getpid()}")
    try:
        tmp.write_text(fresh, encoding="utf-8", newline="\n")
        tmp.replace(OUT)
    finally:
        tmp.unlink(missing_ok=True)
    print(f"wrote {OUT}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

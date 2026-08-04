#!/usr/bin/env python3
"""Build the settlement list at internal/places/data/greece.csv.

    python3 scripts/harvest-places.py            # rebuild, diff, exit 1 if stale
    python3 scripts/harvest-places.py --update   # write the dataset

Why this exists: pickup points were typed as "25.8743, 40.8476 Αλεξανδρούπολη",
which is not a thing anyone will do twice. A person types a town name, so there
has to be a list of town names with coordinates attached.

The source is the same OpenStreetMap extract the router runs on, read locally
with osmium. It used to be the public Overpass API, one query per prefecture.
That was fine for one prefecture and wrong for a country: seventy-odd queries
against a shared free service, whose answers change under you, is a build that
depends on somebody else's afternoon. The extract is already on disk, already
the thing every route is computed against, and reading it twice is free.

Duplicate names are the reason the municipality is harvested too. Greece has
dozens of Άγιος Γεώργιος, and a list showing eight identical rows is no better
than no list. Each settlement is assigned the smallest administrative area that
contains it, by point-in-polygon against the same extract.

Greek OSM tags municipalities at admin_level 5 and 6 inconsistently -- both hold
Δήμος, Δημοτική Ενότητα and Κοινότητα -- so both are read and the smallest match
wins. There is no level that cleanly holds the περιφερειακή ενότητα, which is
why the answer is a δήμος rather than the prefecture a person might expect.

A settlement inside no Greek municipality is dropped and reported. In practice
those are the Bulgarian and Turkish villages the extract's border box reaches
into, plus Mount Athos, which has its own administrative status and no
carpooling.

Output: © OpenStreetMap contributors, ODbL.
"""
import argparse
import csv
import io
import json
import pathlib
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parent.parent
OUT = ROOT / "internal/places/data/greece.csv"
DEFAULT_PBF = ROOT / "osrm-data/greece-latest.osm.pbf"

# hamlet is included and ranked last by the Go side. Leaving it out would mean
# somebody living in one cannot name where they are; including it costs 4,832
# rows that only surface when their name is typed.
KINDS = ("city", "town", "village", "hamlet")

# Both levels, because Greek OSM uses them interchangeably for municipalities.
ADMIN_LEVELS = ("5", "6")

FIELDS = [
    "osm_type", "osm_id", "kind", "name", "name_en", "int_name", "alt_name",
    "area", "area_en", "population", "lon", "lat",
]

# Prefixes stripped from a municipality name for display. "Νέα Χώρα, Δήμος
# Χανίων" is what OSM says; "Νέα Χώρα, Χανίων" is what fits under a search
# result.
AREA_PREFIXES = ("Δημοτική Ενότητα ", "Δημοτική Κοινότητα ", "Κοινότητα ", "Δήμος ")


def osmium(*args):
    try:
        subprocess.run(["osmium", *args], check=True, capture_output=True)
    except FileNotFoundError:
        sys.exit("osmium is not installed: brew install osmium-tool")
    except subprocess.CalledProcessError as e:
        sys.exit(f"osmium {' '.join(args)} failed:\n{e.stderr.decode(errors='replace')}")


def features(pbf: pathlib.Path, tmp: pathlib.Path, filters: list[str], geometry: str):
    """Runs osmium tags-filter then export, and yields GeoJSON features."""
    filtered = tmp / "filtered.osm.pbf"
    exported = tmp / "exported.geojsonseq"
    osmium("tags-filter", str(pbf), *filters, "-o", str(filtered), "--overwrite")
    # -u type_id puts "node/123456" in the properties as @id. Without it the
    # export carries no identity at all, and a name is not an identity: this
    # dataset holds many settlements that share one.
    osmium("export", str(filtered), "-f", "geojsonseq", "-u", "type_id",
           f"--geometry-types={geometry}", "-o", str(exported), "--overwrite")
    with exported.open(encoding="utf-8") as fh:
        for line in fh:
            # geojsonseq separates records with RS (U+001E).
            line = line.lstrip("\x1e").strip()
            if line:
                yield json.loads(line)


def rings(geometry: dict) -> list:
    kind = geometry.get("type")
    if kind == "Polygon":
        return [geometry["coordinates"]]
    if kind == "MultiPolygon":
        return geometry["coordinates"]
    return []


class Area:
    """One municipality, with a bounding box so most points are rejected fast."""

    __slots__ = ("name", "name_en", "polygons", "x0", "y0", "x1", "y1", "span")

    def __init__(self, name, name_en, polygons):
        self.name, self.name_en, self.polygons = name, name_en, polygons
        xs = [x for poly in polygons for ring in poly for x, _ in ring]
        ys = [y for poly in polygons for ring in poly for _, y in ring]
        self.x0, self.y0, self.x1, self.y1 = min(xs), min(ys), max(xs), max(ys)
        # Used to break ties: where two administrative levels both contain a
        # settlement, the smaller one is the more useful thing to show.
        self.span = (self.x1 - self.x0) * (self.y1 - self.y0)

    def contains(self, x, y) -> bool:
        if not (self.x0 <= x <= self.x1 and self.y0 <= y <= self.y1):
            return False
        for poly in self.polygons:
            if in_ring(x, y, poly[0]) and not any(in_ring(x, y, h) for h in poly[1:]):
                return True
        return False


def in_ring(x, y, ring) -> bool:
    """Ray casting. A point exactly on an edge may fall either way; that is
    acceptable here because the answer is a label, not a boundary decision."""
    hit = False
    j = len(ring) - 1
    for i in range(len(ring)):
        xi, yi = ring[i]
        xj, yj = ring[j]
        if (yi > y) != (yj > y) and x < (xj - xi) * (y - yi) / (yj - yi) + xi:
            hit = not hit
        j = i
    return hit


def load_areas(pbf, tmp) -> list[Area]:
    areas = []
    for level in ADMIN_LEVELS:
        for f in features(pbf, tmp / level, [f"r/admin_level={level}"], "polygon"):
            p = f.get("properties", {})
            if p.get("boundary") != "administrative" or not p.get("name"):
                continue
            polygons = rings(f.get("geometry", {}))
            if polygons:
                areas.append(Area(p["name"], p.get("name:en", ""), polygons))
    # Smallest first, so the first containing area found is the most specific.
    areas.sort(key=lambda a: a.span)
    return areas


def population(raw: str) -> str:
    """OSM population values are free text: "12.500", "~3000", "1,234".

    Only the first number is read. Joining every digit in the field turns
    "3000 (2011)" into thirty million and makes a village the largest place in
    Greece, which is the kind of thing that shows up as a strange search result
    long after anybody would think to look here.
    """
    import re

    m = re.search(r"\d[\d.,\s]*", raw)
    if not m:
        return "0"
    digits = "".join(ch for ch in m.group(0) if ch.isdigit())
    return digits if digits else "0"


def strip_prefix(name: str) -> str:
    for prefix in AREA_PREFIXES:
        if name.startswith(prefix):
            return name[len(prefix):]
    return name


def build(pbf: pathlib.Path) -> tuple[list[dict], list[str]]:
    with tempfile.TemporaryDirectory() as td:
        tmp = pathlib.Path(td)
        for level in ADMIN_LEVELS:
            (tmp / level).mkdir()
        (tmp / "places").mkdir()

        areas = load_areas(pbf, tmp)
        print(f"{len(areas)} administrative areas", file=sys.stderr)

        rows, orphans = [], []
        for f in features(pbf, tmp / "places",
                          [f"n/place={','.join(KINDS)}"], "point"):
            p = f.get("properties", {})
            name = p.get("name")
            coords = f.get("geometry", {}).get("coordinates")
            if not name or not coords:
                continue
            lon, lat = coords[0], coords[1]

            area = next((a for a in areas if a.contains(lon, lat)), None)
            if area is None:
                orphans.append(name)
                continue

            rows.append({
                "osm_type": "node",
                # osmium renders a type_id as "n441183": one letter, then the
                # number. Only nodes are harvested, so the letter is dropped.
                "osm_id": str(f.get("id", ""))[1:],
                "kind": p["place"],
                "name": name,
                "name_en": p.get("name:en", ""),
                "int_name": p.get("int_name", ""),
                "alt_name": p.get("alt_name", ""),
                "area": strip_prefix(area.name),
                "area_en": area.name_en,
                # Present on 50 of the 54 cities and about half the villages.
                # It is what tells Alexandroupoli from Alexandreia when both
                # match what was typed; without it the tie fell to whichever
                # name was shorter, which is not a fact about anywhere.
                "population": population(p.get("population", "")),
                # Six decimals is about ten centimetres, far finer than a
                # settlement centre is known to.
                "lon": f"{lon:.6f}",
                "lat": f"{lat:.6f}",
            })

    # Sorted so that a rebuild from the same extract produces the same bytes,
    # and a diff shows what OSM changed rather than what osmium reordered.
    rows.sort(key=lambda r: (r["name"], r["area"], r["osm_id"]))
    return rows, orphans


def render(rows: list[dict]) -> str:
    buf = io.StringIO()
    w = csv.DictWriter(buf, fieldnames=FIELDS, lineterminator="\n")
    w.writeheader()
    w.writerows(rows)
    return buf.getvalue()


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--pbf", type=pathlib.Path, default=DEFAULT_PBF)
    ap.add_argument("--update", action="store_true", help="write the dataset")
    args = ap.parse_args()

    if not args.pbf.exists():
        sys.exit(f"{args.pbf} not found; download the Greece extract from Geofabrik")

    rows, orphans = build(args.pbf)
    print(f"{len(rows)} settlements, {len(orphans)} outside any Greek municipality",
          file=sys.stderr)
    if orphans:
        print("  dropped: " + ", ".join(sorted(set(orphans))[:12]), file=sys.stderr)

    rendered = render(rows)
    if args.update:
        OUT.parent.mkdir(parents=True, exist_ok=True)
        OUT.write_text(rendered, encoding="utf-8")
        print(f"wrote {OUT}", file=sys.stderr)
        return 0

    current = OUT.read_text(encoding="utf-8") if OUT.exists() else ""
    if current == rendered:
        print("dataset is current", file=sys.stderr)
        return 0
    print("dataset is stale; rerun with --update", file=sys.stderr)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())

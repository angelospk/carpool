#!/usr/bin/env python3
"""Build the meeting-point list at internal/places/data/meeting-points.csv.

    python3 scripts/harvest-meeting-points.py            # rebuild and diff
    python3 scripts/harvest-meeting-points.py --update   # write the dataset

"Where exactly?" is the question a pickup point has to answer, and a settlement
centre does not answer it. A village node in OSM is a label position: it can sit
in a field. Two people who have agreed on "Feres" still have to agree on where
in Feres, and they do it over the phone, badly.

So each settlement gets a small number of named landmarks a driver can be told
to wait at. They are ranked by how findable they are rather than how close they
are: everybody in a Greek village knows the petrol station, and a car can
actually stop there.

What is deliberately NOT here is a fallback. An earlier plan put a synthetic
"Κέντρο <village>" at the settlement node wherever nothing was tagged, which is
exactly the wrong thing: it is a coordinate with a confident name on it, and the
one promise a meeting point makes is that it can be found. A settlement with no
landmark gets none, and the app says so.

Rejected on purpose:

- Bus stops and platforms. There are 17,000 of them and a bus stop is not
  somewhere a car waits.
- Anything with access=private, customers or no. A gated car park is not a
  place to meet a stranger.
- Anything unnamed. "The parking" is not a landmark.
- Anything that is a bus stop wearing a station's tag: highway=bus_stop, or a
  route_ref listing the urban lines it serves. Θεσσαλονίκη's list was three of
  these, and one of them was a coach agency.
- Names that are really route numbers — "ΚΕΝ. ΑΦΕΤΗΡΙΑ Νο2, Νο4, Νο5" is a
  terminus board, not a place.

What is NOT rejected, having been checked against the data rather than assumed:

- shop=ticket. It is on ΚΤΕΛ Αττικής, which is a real coach station with a
  ticket window in it. Treating it as a hard negative would have deleted one of
  the best entries in the file.
- public_transport=station. It is on the junk as often as on the stations,
  including on the coach agency, so it discriminates nothing.

What sorts the rest is whether anything says a real establishment is there: a
phone, a website, a street address, a network. A coach agency mapped as a bare
name has none of them; ΚΤΕΛ Λάρισας has four.

Output: © OpenStreetMap contributors, ODbL.
"""
import argparse
import unicodedata
import csv
import io
import math
import pathlib
import re
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parent.parent
OUT = ROOT / "internal/places/data/meeting-points.csv"
PLACES = ROOT / "internal/places/data/greece.csv"
DEFAULT_PBF = ROOT / "osrm-data/greece-latest.osm.pbf"

FIELDS = ["place_id", "osm_type", "osm_id", "rank", "kind", "name", "name_en", "lon", "lat"]

# Ranked by how findable the thing is, not by how close. Everybody in a Greek
# village can point at the petrol station; nobody can point at car park #3.
#
# Nodes only, for the same reason harvest-places.py takes only nodes: the centre
# of a way's bounding box is not guaranteed to sit inside the feature, let alone
# somewhere a car can stop. It does cost coverage — plenty of petrol stations
# are mapped as a building outline — and that is the trade being made, not an
# oversight. The osm_type column is written anyway, because an id without its
# type is not an identity if ways are ever added.
KINDS = [
    ("bus_station", ["n/amenity=bus_station"]),
    ("train_station", ["n/railway=station", "n/railway=halt"]),
    ("port", ["n/amenity=ferry_terminal"]),
    ("fuel", ["n/amenity=fuel"]),
    ("parking", ["n/amenity=parking"]),
]

# How many landmarks a settlement of each kind is worth listing. A city needs
# more than one because "the petrol station" is ambiguous in Thessaloniki; a
# village needs one because there is one.
PER_KIND = {"city": 3, "town": 2, "village": 1, "hamlet": 1}

# How far a landmark can be from the settlement centre and still be that
# settlement's. Generous for a city, tight for a hamlet, because a hamlet three
# kilometres from a petrol station does not have a petrol station.
RADIUS_KM = {"city": 6.0, "town": 4.0, "village": 2.5, "hamlet": 2.0}

# Access values that mean "not for you". A gated car park is not a place to meet
# a stranger, and offering it is worse than offering nothing.
CLOSED_ACCESS = {"private", "customers", "no", "permit", "residents"}

# Tags that mean "this is a stop, whatever its amenity says". A thing serving
# numbered urban routes is where a bus pauses, not where a car waits.
STOP_TELLS = ("highway", "route_ref", "public_transport:version")
STOP_HIGHWAY = {"bus_stop", "platform"}

# Names that are timetables. Route numbers, arrows to destinations, and the
# semicolon-separated line lists that come with them.
ROUTE_NOISE = re.compile(r"(Νο\s?\d|Ν[οο]\.\s?\d|ΓΡΑΜΜΗ\s?\d|->|→|\bNo\.?\s?\d)", re.IGNORECASE)

# Words that name a piece of the urban bus network rather than a place a car
# meets somebody. Αφετηρία is where a city line starts, αμαξοστάσιο is the depot
# it sleeps in, and Τ.Σ. is how στάση is abbreviated on a pole.
#
# "Αστικ-" is deliberately not here on its own: ΚΤΕΛ Αστικών Λάρισας is the
# operator of the city buses and also a real station somebody can be dropped at.
URBAN_NETWORK = re.compile(r"(Αφετηρ|Αμαξοστάσ|\bΤ\.\s?Σ\.|\bΤ\.Σ\b)", re.IGNORECASE)

# A ferry terminal named after the boats tied up at it. Two vessels joined with
# an ampersand is a berth label, not a place.
VESSEL_NAME = re.compile(r"&")

# Tags that say somebody actually runs something here. None of them proves a
# coach station; together they separate one from a name on a pavement.
ESTABLISHMENT_TELLS = ("phone", "website", "operator", "network", "addr:street",
                       "opening_hours", "email", "brand")

# The intercity coach network. In Greece the thing everybody means by "the bus
# station" is the ΚΤΕΛ, and a bus_station that says so is that; one that does
# not is a private agency, an urban terminus, or a ticket window. This is in the
# name rather than in the tags, which is why an earlier pass concluded no
# automated rule could separate Θεσσαλονίκη's coach agencies from its ΚΤΕΛ.
# Written ΚΤΕΛ, KTEL, or Κ.Τ.Ε.Λ. with any spacing between the dots. Ρόδος
# spells it the third way and would otherwise not be recognised as a coach
# station at all.
COACH_NETWORK = re.compile(r"\b[ΚK]\.?\s?[ΤT]\.?\s?[ΕE]\.?\s?[ΛL]\b\.?", re.IGNORECASE)

# A ticket window is not a place a car waits, but it is often mapped with the
# station's own name and the network's, so it cannot simply be dropped: ΚΤΕΛ
# Αττικής is a real station with a ticket office in it. Ranked below instead.
TICKET_DESK = re.compile(r"ticket|εκδοτήρι|πρακτορε", re.IGNORECASE)


def stem(word):
    """A Greek word without its accents, its case, or its last two letters.

    Greek inflects, and the settlement is Θεσσαλονίκη while its station is
    Θεσσαλονίκης. Comparing whole words finds nothing. Two letters is enough to
    cover the genitive endings that matter here without collapsing short names
    into each other.
    """
    folded = unicodedata.normalize("NFD", word.casefold())
    bare = "".join(c for c in folded if not unicodedata.combining(c))
    return bare[:-2] if len(bare) > 5 else bare


def names_this_place(name, settlement):
    """Whether a point's name mentions the settlement it is being attached to,
    or the prefecture it is in.

    A station named after somewhere else is somewhere else's station that
    happens to have an office here — ΚΤΕΛ Καρδίτσας-Τρικάλων sits in Λάρισα,
    and putting it above Λάρισα's own is how a passenger ends up at the wrong
    kerb.
    """
    haystack = {stem(w) for w in re.split(r"[^\w]+", name) if len(w) > 3}
    for field in ("name", "area"):
        for word in re.split(r"[^\w]+", settlement.get(field, "")):
            if len(word) > 3 and stem(word) in haystack:
                return True
    return False


def load_settlements() -> list[dict]:
    with PLACES.open(encoding="utf-8") as fh:
        return list(csv.DictReader(fh))


def haversine_km(lon1, lat1, lon2, lat2) -> float:
    r = 6371.0
    p1, p2 = math.radians(lat1), math.radians(lat2)
    dp = p2 - p1
    dl = math.radians(lon2 - lon1)
    a = math.sin(dp / 2) ** 2 + math.cos(p1) * math.cos(p2) * math.sin(dl / 2) ** 2
    return 2 * r * math.asin(math.sqrt(a))


def build(pbf: pathlib.Path, features) -> tuple[list[dict], dict]:
    settlements = load_settlements()

    # A coarse grid so each landmark is only compared with nearby settlements.
    # A degree is about 111 km, so a tenth of a degree is a cell of roughly ten
    # kilometres — comfortably wider than the largest radius above.
    cell = 0.1
    grid: dict[tuple[int, int], list[dict]] = {}
    for s in settlements:
        s["lon_f"], s["lat_f"] = float(s["lon"]), float(s["lat"])
        grid.setdefault((int(s["lon_f"] / cell), int(s["lat_f"] / cell)), []).append(s)

    def nearby(lon, lat):
        cx, cy = int(lon / cell), int(lat / cell)
        for dx in (-1, 0, 1):
            for dy in (-1, 0, 1):
                yield from grid.get((cx + dx, cy + dy), ())

    # place_id -> list of (rank, distance, row)
    found: dict[str, list] = {}

    with tempfile.TemporaryDirectory() as td:
        tmp = pathlib.Path(td)
        for rank, (kind, filters) in enumerate(KINDS):
            work = tmp / kind
            work.mkdir()
            for f in features(pbf, work, filters, "point"):
                p = f.get("properties", {})
                name = p.get("name")
                coords = f.get("geometry", {}).get("coordinates")
                if not name or not coords:
                    continue
                if p.get("access", "") in CLOSED_ACCESS:
                    continue
                if p.get("highway") in STOP_HIGHWAY or "route_ref" in p:
                    continue
                if ROUTE_NOISE.search(name) or URBAN_NETWORK.search(name):
                    continue
                if kind == "port" and VESSEL_NAME.search(name):
                    continue

                # Two candidates of the same kind are separated by whether
                # anything says a real establishment is there. Without this a
                # coach agency mapped as a bare name outranks ΚΤΕΛ, because both
                # are amenity=bus_station and the agency happens to sit nearer
                # the settlement node.
                established = any(tell in p for tell in ESTABLISHMENT_TELLS)

                lon, lat = coords[0], coords[1]
                best, best_km = None, None
                for s in nearby(lon, lat):
                    km = haversine_km(lon, lat, s["lon_f"], s["lat_f"])
                    if km > RADIUS_KM.get(s["kind"], 2.0):
                        continue
                    if best_km is None or km < best_km:
                        best, best_km = s, km
                if best is None:
                    continue

                place_id = f"{best['osm_type']}/{best['osm_id']}"
                # osmium renders a type_id as "n441183" or "w88213": one
                # letter, then the number. Both are harvested -- a petrol
                # station is as often a building outline as a point -- and an
                # id without its type is not an identity, because the two
                # spaces overlap.
                raw = str(f.get("id", ""))
                # Three signals that only mean anything for a coach station,
                # ordered by how badly getting them wrong sends somebody to the
                # wrong kerb. All are 0 for every other kind, so nothing else
                # is reordered by them.
                coach = ticket = elsewhere = 0
                if kind == "bus_station":
                    # Ordered by how badly getting it wrong sends somebody to
                    # the wrong kerb. Naming another prefecture is the worst:
                    # ΚΤΕΛ Καρδίτσας-Τρικάλων has an office in Λάρισα, and it is
                    # still Καρδίτσα's station. Being a ΚΤΕΛ at all comes next,
                    # then whether the thing is a ticket window.
                    elsewhere = 0 if names_this_place(name, best) else 1
                    coach = 0 if COACH_NETWORK.search(name) else 1
                    ticket = 1 if TICKET_DESK.search(name) else 0

                found.setdefault(place_id, []).append((rank, 0 if established else 1,
                                                       elsewhere, coach, ticket, best_km, {
                    "place_id": place_id,
                    "osm_type": {"n": "node", "w": "way", "r": "relation"}.get(raw[:1], "node"),
                    "osm_id": raw[1:],
                    # Overwritten below with the position this point ends up
                    # in, once the candidates for the settlement have been
                    # sorted. It starts as the kind index because that is the
                    # first term of the sort.
                    #
                    # This used to be the final value, and that was the bug
                    # behind "Θεσσαλονίκη shows the ticket agencies": every bus
                    # station in a city shared one rank, so the careful
                    # ordering computed here was thrown away and the app fell
                    # back to sorting by osm_id. The ranking was never the
                    # problem; keeping it was.
                    "rank": str(rank),
                    "kind": kind,
                    "name": name,
                    "name_en": p.get("name:en", ""),
                    "lon": f"{lon:.6f}",
                    "lat": f"{lat:.6f}",
                }))

    by_kind = {s["osm_type"] + "/" + s["osm_id"]: s["kind"] for s in settlements}
    rows = []
    for place_id, candidates in found.items():
        candidates.sort(key=lambda c: (c[:-1], c[-1]["osm_id"]))
        limit = PER_KIND.get(by_kind.get(place_id, "village"), 1)

        # One name per settlement. Ηράκλειο had two ferry terminals both called
        # "Heraklion", which is a choice between two identical answers.
        taken, seen = [], set()
        for c in candidates:
            key = c[-1]["name"].strip().casefold()
            if key in seen:
                continue
            seen.add(key)
            row = c[-1]
            # The position, so the order computed above is what the app reads.
            row["rank"] = str(len(taken))
            taken.append(row)
            if len(taken) == limit:
                break
        rows.extend(taken)

    # Sorted for a reproducible diff, but on the rank first so the file order
    # is the order the app shows them in.
    rows.sort(key=lambda r: (r["place_id"], int(r["rank"]), r["osm_id"]))

    covered = len(found)
    stats = {
        "settlements": len(settlements),
        "covered": covered,
        "bare": len(settlements) - covered,
        "points": len(rows),
    }
    return rows, stats


def render(rows: list[dict]) -> str:
    buf = io.StringIO()
    w = csv.DictWriter(buf, fieldnames=FIELDS, lineterminator="\n")
    w.writeheader()
    w.writerows(rows)
    return buf.getvalue()


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--pbf", type=pathlib.Path, default=DEFAULT_PBF)
    ap.add_argument("--update", action="store_true")
    args = ap.parse_args()

    if not args.pbf.exists():
        sys.exit(f"{args.pbf} not found")
    if not PLACES.exists():
        sys.exit(f"{PLACES} not found; run harvest-places.py first")

    # Reuses the osmium plumbing rather than repeating it, so both datasets are
    # read out of the same extract in the same way.
    spec = import_module_from(ROOT / "scripts/harvest-places.py")
    rows, stats = build(args.pbf, spec.features)

    print(f"{stats['points']} meeting points for {stats['covered']} of "
          f"{stats['settlements']} settlements; {stats['bare']} have none", file=sys.stderr)

    rendered = render(rows)
    if args.update:
        OUT.write_text(rendered, encoding="utf-8")
        print(f"wrote {OUT}", file=sys.stderr)
        return 0

    current = OUT.read_text(encoding="utf-8") if OUT.exists() else ""
    if current == rendered:
        print("dataset is current", file=sys.stderr)
        return 0
    print("dataset is stale; rerun with --update", file=sys.stderr)
    return 1


def import_module_from(path: pathlib.Path):
    import importlib.util
    spec = importlib.util.spec_from_file_location("harvest_places", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


if __name__ == "__main__":
    raise SystemExit(main())

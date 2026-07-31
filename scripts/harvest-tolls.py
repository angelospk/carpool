#!/usr/bin/env python3
"""Refresh the toll dataset at internal/tolls/data/greece.json.

    python3 scripts/harvest-tolls.py             # dry run: fetch and diff
    python3 scripts/harvest-tolls.py --write     # write the dataset

The source answers "which toll stations are on this route", and has no endpoint
for the whole network. So the network is covered by routing along every Greek
motorway corridor in both directions and taking the union. Both directions
matter: 25 stations are co-located pairs, one record per carriageway, and a
route must be charged for exactly one of the two.

Each station's direction of travel is measured the way the matcher measures it —
projected onto the route, heading taken over a 60 m chord either side. Taking
the bearing at the nearest vertex instead put Rio-Antirrio 113 degrees out,
because the nearest vertex belonged to an approach road.

Prices are published by the motorway operators; this only collects them. Run it
a few times a year, when tariffs change. It sleeps between requests on purpose.
"""

import argparse, json, math, os, sys, time, urllib.parse, urllib.request

BASE = "https://vriskoapostasi.gr/ajax/ajaxItalyNewOSRM.php"
HDRS = {
    "Referer": "https://vriskoapostasi.gr/el/search",
    "X-Requested-With": "XMLHttpRequest",
    "User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120 Safari/537.36",
}

def route(a, b, alternatives=2):
    q = {
        "fromNames": urllib.parse.quote("A"), "toNames": urllib.parse.quote("B"),
        "fromCoords": f"{a[0]},{a[1]}", "toCoords": f"{b[0]},{b[1]}",
        "middleCoords": "", "testDiodia": "-1", "routingProvider": "3",
        "maxAlternatives": str(alternatives),
        "currentUrl": "https://vriskoapostasi.gr/el/search",
    }
    url = BASE + "?" + urllib.parse.urlencode(q)
    req = urllib.request.Request(url, headers=HDRS)
    with urllib.request.urlopen(req, timeout=60) as r:
        return json.loads(r.read().decode())



def bearing(a, b):
    """Compass bearing from a to b, in degrees."""
    lat1, lon1, lat2, lon2 = map(math.radians, (*a, *b))
    dl = lon2 - lon1
    y = math.sin(dl) * math.cos(lat2)
    x = math.cos(lat1) * math.sin(lat2) - math.sin(lat1) * math.cos(lat2) * math.cos(dl)
    return (math.degrees(math.atan2(y, x)) + 360) % 360


def decode(poly, precision=5):
    """Standard Google/OSRM polyline decode."""
    coords, index, lat, lng, factor = [], 0, 0, 0, 10 ** precision
    while index < len(poly):
        for target in ('lat', 'lng'):
            shift, result = 0, 0
            while True:
                b = ord(poly[index]) - 63
                index += 1
                result |= (b & 0x1f) << shift
                shift += 5
                if b < 0x20:
                    break
            d = ~(result >> 1) if result & 1 else (result >> 1)
            if target == 'lat':
                lat += d
            else:
                lng += d
        coords.append((lat / factor, lng / factor))
    return coords


R = 6371008.8
def rad(d): return d*math.pi/180

def seg_dist_m(lat, lon, a, b):
    """Distance to the segment, and how far along it (0..1) the closest point is."""
    mx = 111320*math.cos(rad(lat)); my = 110540.0
    ax, ay = (a[1]-lon)*mx, (a[0]-lat)*my
    bx, by = (b[1]-lon)*mx, (b[0]-lat)*my
    dx, dy = bx-ax, by-ay
    if dx == 0 and dy == 0: return math.hypot(ax, ay), 0.0
    t = max(0, min(1, (-ax*dx - ay*dy)/(dx*dx+dy*dy)))
    return math.hypot(ax+t*dx, ay+t*dy), t

def cumulative(pts):
    out=[0.0]
    for i in range(1,len(pts)):
        φ1,φ2 = rad(pts[i-1][0]), rad(pts[i][0])
        dφ, dλ = rad(pts[i][0]-pts[i-1][0]), rad(pts[i][1]-pts[i-1][1])
        h = math.sin(dφ/2)**2 + math.cos(φ1)*math.cos(φ2)*math.sin(dλ/2)**2
        out.append(out[-1] + 2*R*math.asin(min(1, math.sqrt(h))))
    return out

def point_at(pts, cum, at):
    if at <= 0: return pts[0]
    if at >= cum[-1]: return pts[-1]
    lo, hi = 0, len(cum)-1
    while lo < hi:
        mid = (lo+hi)//2
        if cum[mid] < at: lo = mid+1
        else: hi = mid
    i = max(1, lo)
    span = cum[i]-cum[i-1]
    f = 0 if span == 0 else (at-cum[i-1])/span
    return (pts[i-1][0]+f*(pts[i][0]-pts[i-1][0]), pts[i-1][1]+f*(pts[i][1]-pts[i-1][1]))

CHORD = 60.0

def travel_bearing(pts, cum, station):
    """Heading of travel where the route passes closest to the station."""
    best, bi, bt = 1e18, 0, 0.0
    for i in range(len(pts)-1):
        d, t = seg_dist_m(station[0], station[1], pts[i], pts[i+1])
        if d < best: best, bi, bt = d, i, t
    # Centred on the projected point. Motorway geometry runs kilometres between
    # vertices, so the segment's start vertex can be a different road entirely.
    at = cum[bi] + bt*(cum[bi+1]-cum[bi])
    return bearing(point_at(pts, cum, at-CHORD), point_at(pts, cum, at+CHORD)), best


CORRIDORS = [
    ("A1 PATHE", (38.0455, 23.7854), (40.6401, 22.9444)),
    ("A2 Egnatia west", (39.5036, 20.2597), (40.6401, 22.9444)),
    ("A2 Egnatia east", (40.6401, 22.9444), (41.0246, 26.1103)),
    ("A8 Olympia", (38.0455, 23.7854), (38.2466, 21.7346)),
    ("A5 Ionia", (38.3320, 21.7712), (39.6650, 20.8537)),
    ("A7 Moreas", (37.9382, 22.9330), (37.0389, 22.1142)),
    ("A7 Tripoli-Sparti", (37.5089, 22.3794), (37.0739, 22.4294)),
    ("A6 Attiki Odos", (38.0736, 23.5407), (37.9364, 23.9445)),
    ("A24 Evzonoi", (40.6401, 22.9444), (41.0836, 22.5539)),
    ("A25 Promachonas", (40.6401, 22.9444), (41.3775, 23.3450)),
    ("A29 Kastoria", (40.2621, 21.5502), (40.5167, 21.2667)),
    ("A27 Niki", (40.5167, 21.2667), (40.7833, 21.4000)),
    ("Rio-Antirrio", (38.2466, 21.7346), (38.3320, 21.7712)),
    ("Aktio-Preveza", (38.8780, 20.7620), (38.9560, 20.7510)),
    ("A3 Kentriki", (38.9039, 22.5317), (39.6390, 21.6300)),
    ("Egnatia-Kipoi", (41.1214, 25.4058), (41.0246, 26.1103)),
    ("A1 north leg", (39.6390, 22.4194), (40.6401, 22.9444)),
    ("A8 Corinth", (37.9382, 22.9330), (38.0455, 23.7854)),
]

EXTRA = [
    ("Orestiada-Alex", (41.5030, 26.5300), (40.8457, 25.8740)),
    ("Athens-Airport", (37.9838, 23.7275), (37.9364, 23.9445)),
    ("Trikala-Ioannina", (39.5550, 21.7680), (39.6650, 20.8537)),
    ("Nafplio-Athens", (37.5680, 22.8000), (38.0455, 23.7854)),
    ("Volos-Larisa", (39.3620, 22.9420), (39.6390, 22.4194)),
    ("Athens-Elefsina", (38.0455, 23.7854), (38.0400, 23.5400)),
    ("Lamia-Karpenisi", (38.9000, 22.4340), (38.9130, 21.7900)),
    ("Kalambaka-Larisa", (39.7050, 21.6280), (39.6390, 22.4194)),
    ("Xanthi-Komotini", (41.1350, 24.8880), (41.1214, 25.4058)),
    ("Drama-Kavala", (41.1520, 24.1470), (40.9396, 24.4069)),
    ("Serres-Drama", (41.0850, 23.5480), (41.1520, 24.1470)),
    ("Katerini-Thess", (40.2700, 22.5060), (40.6401, 22.9444)),
    ("Corinth-Patra", (37.9382, 22.9330), (38.2466, 21.7346)),
    ("Igoumenitsa-Preveza", (39.5036, 20.2597), (38.9560, 20.7510)),
    ("Kozani-Larisa", (40.3007, 21.7887), (39.6390, 22.4194)),
    ("Tripoli-Athens", (37.5089, 22.3794), (38.0455, 23.7854)),
]


def cents(x):
    return int(round(float(x) * 100))


def dedupe_bearings(bs):
    """Collapse repeated measurements, keeping genuinely distinct directions."""
    out = []
    for b in sorted(set(bs)):
        if all(min(abs(b - o), 360 - abs(b - o)) > 40 for o in out):
            out.append(round(b))
    return out


def harvest(verbose=True):
    stations = {}
    for name, a, b in CORRIDORS + EXTRA:
        for label, (p, q) in (("AB", (a, b)), ("BA", (b, a))):
            try:
                d = route(p, q, alternatives=0)
            except Exception as e:
                print(f"  ! {name} {label}: {e}", file=sys.stderr)
                continue
            rt = d["routes"][0]
            pts = decode(rt["geometry"])
            cum = cumulative(pts)
            found = d["tolls"][0][0] if d.get("tolls") and d["tolls"][0] else []
            for s in found:
                brg, dist = travel_bearing(pts, cum, (s[2], s[3]))
                st = stations.setdefault(s[9], {
                    "id": s[9], "name_el": s[0].strip(), "short_el": s[1].strip(),
                    "name_en": s[10], "lat": s[2], "lon": s[3],
                    "car": s[5], "motorcycle": s[6], "truck2": s[7], "truck3": s[8],
                    "operator": s[15], "operator_el": s[18],
                    "bearings": [], "approach_m": [],
                })
                st["bearings"].append(round(brg, 1))
                st["approach_m"].append(round(dist, 1))
            if verbose:
                print(f"{name:22} {label}: {len(found):2} tolls (total {len(stations)})")
            time.sleep(1.2)
    return stations


PLAZA_M = 200.0  # two records closer than this are one plaza, wider than any real pair


def plaza_groups(raw):
    """One group per plaza, clustered by distance.

    Rounding the coordinates into a bucket instead put twelve plazas' two
    carriageways in different groups: records fifteen metres apart landed either
    side of a bucket boundary, and the pairing that stops a route being charged
    for the barrier on the other carriageway was silently off for them.
    """
    groups, centres = {}, []
    for s in raw:
        for gid, (clat, clon) in enumerate(centres):
            d = math.hypot((s["lat"]-clat)*110540,
                           (s["lon"]-clon)*111320*math.cos(rad(clat)))
            if d <= PLAZA_M:
                groups[s["id"]] = gid
                break
        else:
            groups[s["id"]] = len(centres)
            centres.append((s["lat"], s["lon"]))
    return groups


def build(raw, harvested_at):
    # Co-located records are the two carriageways of one plaza. Grouping them is
    # what stops a route being charged for the barrier on the other side.
    groups = plaza_groups(raw)

    stations = [{
        "id": s["id"],
        "group": groups[s["id"]],
        "name_el": s["name_el"], "name_en": s["name_en"], "short_el": s["short_el"],
        "lat": round(s["lat"], 7), "lon": round(s["lon"], 7),
        "bearings": dedupe_bearings(s["bearings"]),
        "operator": s["operator"], "operator_el": s["operator_el"],
        "prices_cents": {
            "car": cents(s["car"]), "motorcycle": cents(s["motorcycle"]),
            "truck2": cents(s["truck2"]), "truck3": cents(s["truck3"]),
        },
    } for s in raw]

    return {
        "schema": 1,
        "harvested_at": harvested_at,
        "source": "vriskoapostasi.gr /ajax/ajaxItalyNewOSRM.php; prices are published by the motorway operators themselves",
        "note": "Cash list prices per vehicle class. Electronic-pass discounts and permanent-resident exemptions are not modelled; the figure is an estimate, not a receipt.",
        "stations": sorted(stations, key=lambda s: s["id"]),
    }


def review(old, new):
    """Everything a human should look at before trusting a refresh."""
    o = {s["id"]: s for s in old.get("stations", [])}
    n = {s["id"]: s for s in new["stations"]}
    problems, notes = [], []

    for sid in sorted(set(o) - set(n)):
        problems.append(f"station {sid} ({o[sid]['short_el']}) disappeared")
    for sid in sorted(set(n) - set(o)):
        notes.append(f"new station {sid} ({n[sid]['short_el']})")

    for sid in sorted(set(o) & set(n)):
        a, b = o[sid], n[sid]
        moved = math.hypot((a["lat"] - b["lat"]) * 110540,
                           (a["lon"] - b["lon"]) * 111320 * math.cos(math.radians(a["lat"])))
        if moved > 50:
            problems.append(f"station {sid} ({b['short_el']}) moved {moved:.0f} m")
        if sorted(a["bearings"]) != sorted(b["bearings"]):
            problems.append(f"station {sid} ({b['short_el']}) changed direction: "
                            f"{a['bearings']} -> {b['bearings']}")
        for cls in ("car", "motorcycle", "truck2", "truck3"):
            was, now = a["prices_cents"][cls], b["prices_cents"][cls]
            if was == now:
                continue
            if now < was or now > was * 1.25:
                problems.append(f"station {sid} ({b['short_el']}) {cls}: "
                                f"{was/100:.2f} -> {now/100:.2f} EUR")
            else:
                notes.append(f"station {sid} ({b['short_el']}) {cls}: "
                             f"{was/100:.2f} -> {now/100:.2f} EUR")

    # A price of -1 means the source does not know it; the matcher must not
    # silently treat that as free.
    for sid, s in n.items():
        if any(v < 0 for v in s["prices_cents"].values()):
            notes.append(f"station {sid} ({s['short_el']}) has an unknown price")

    return problems, notes


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--write", action="store_true", help="write the dataset")
    ap.add_argument("--date", default=time.strftime("%Y-%m-%d"))
    ap.add_argument("--out", default="internal/tolls/data/greece.json")
    args = ap.parse_args()

    raw = harvest()
    if not raw:
        print("harvested nothing; leaving the dataset alone", file=sys.stderr)
        return 1
    new = build(sorted(raw.values(), key=lambda s: s["id"]), args.date)

    old = {}
    if os.path.exists(args.out):
        old = json.load(open(args.out))

    problems, notes = review(old, new)
    print(f"\n{len(new['stations'])} stations "
          f"(was {len(old.get('stations', []))})")
    for n in notes:
        print(f"  note: {n}")
    for p in problems:
        print(f"  REVIEW: {p}")

    if problems and not args.write:
        print("\nrefusing a silent refresh: read the lines above, then pass --write")
        return 1
    if not args.write:
        print("\ndry run; pass --write to update the dataset")
        return 0

    with open(args.out, "w") as f:
        json.dump(new, f, ensure_ascii=False, indent=1)
        f.write("\n")
    print(f"\nwrote {args.out} — now run: go test ./internal/tolls/")
    return 0


if __name__ == "__main__":
    sys.exit(main())

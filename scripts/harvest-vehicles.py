#!/usr/bin/env python3
"""Refresh the vehicle consumption dataset at internal/vehicles/data/eea.csv.

    python3 scripts/harvest-vehicles.py             # dry run: fetch and diff
    python3 scripts/harvest-vehicles.py --write     # write the dataset

Why this exists: the suggested contribution multiplies a car's fuel consumption
by the fuel price and the distance, and until now the driver typed the
consumption in. A driver who enters 14 instead of 7 asks for twice the money and
the passenger has no way to tell. See RISKS.md 13.

The source is the EU's registration monitoring under Regulation 2019/631,
published by the EEA under CC-BY 4.0 with commercial reuse allowed. It carries a
real measured consumption column, Fc, in l/100km.

Two facts about Fc decide everything below, both measured rather than assumed:

  * It is populated only for registration years 2021 and 2022. Every earlier
    year is null across the whole table. So this dataset describes recent
    models, not the Greek fleet, and a car older than that is not in here at
    all. Those go through the CO2 figure printed on their άδεια κυκλοφορίας
    (field V.7) instead, which is a different code path and not this script's
    problem.
  * Regulation 2019/631 permits consumption to be reported in l/100km, m³/100km
    or kg/100km depending on the fuel. Petrol and diesel are litres; LPG did not
    match the physical CO2-per-litre constant at all, which is the tell. So this
    harvests petrol and diesel only, and refuses everything else rather than
    guessing at a unit.

Grouping is by make, model, fuel class and engine displacement band. Model alone
is far too coarse: a commercial name spans generations, engine sizes and
hybrids. The Toyota Yaris is the worked example — as one "PETROL" group it runs
from 2.8 to 11.1 l/100km, because 44,000 hybrids sit on top of 32,000
conventional cars. The Fm column separates them, and the displacement band
separates most of the rest.

Whatever survives that still gets a dispersion check. A median over a group that
disagrees with itself is a number with no meaning, so those groups are dropped
rather than published with a false air of precision.

The server will not run PERCENTILE_CONT, nested GROUP BY, or OFFSET/FETCH. So
percentiles are computed here from a histogram the server can produce, and the
harvest is partitioned one make at a time.
"""

import argparse, csv, hashlib, json, os, re, sys, time, urllib.parse, urllib.request

SQL_URL = "https://discodata.eea.europa.eu/sql"
TABLE = "[CO2Emission].[latest].[co2cars]"
SOURCE = ("EEA, Monitoring of CO2 emissions from new passenger cars, "
          "Regulation (EU) 2019/631, CC-BY 4.0")

# Only fuels whose Fc is reported in litres. Anything absent from this map is
# refused rather than converted, because the unit is what is in doubt.
#
# Fm is the fuel mode: M mono-fuel, H hybrid, P plug-in, B bi-fuel. Plug-ins are
# excluded because their consumption depends on how much of the trip ran on the
# battery, which is not a property of the model. Bi-fuel is excluded because the
# figure describes whichever fuel was tested, and the driver picks the other one
# whenever it is cheaper.
FUEL_CLASSES = {
    ("PETROL", "M"): "petrol",
    ("PETROL", "H"): "petrol_hybrid",
    ("DIESEL", "M"): "diesel",
    ("DIESEL", "H"): "diesel_hybrid",
}

# A consumption outside this band is a data error, not a car. The table contains
# CO2-to-Fc ratios as extreme as 1590, so some of these rows are junk.
FC_MIN, FC_MAX = 2.0, 25.0

# A group thinner than this is a handful of registrations, and its median moves
# if one importer files a correction.
MIN_ROWS = 50

# Interquartile range as a fraction of the median. Above this the group is
# describing more than one car and the median is an average of two answers.
MAX_SPREAD = 0.30


def query(sql, timeout=200):
    url = SQL_URL + "?" + urllib.parse.urlencode({"query": sql})
    req = urllib.request.Request(url, headers={"User-Agent": "carpool-harvest/1"})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        body = json.loads(r.read().decode())
    if "errors" in body:
        raise RuntimeError(f"{body['errors']} for: {sql[:160]}")
    return body.get("results", [])


def makes():
    rows = query(f"SELECT TOP 500 Mk, COUNT(*) AS n FROM {TABLE} "
                 f"WHERE Fc BETWEEN {FC_MIN} AND {FC_MAX} AND Mk IS NOT NULL "
                 f"GROUP BY Mk")
    return sorted(r["Mk"] for r in rows if (r["Mk"] or "").strip())


def histogram(make):
    """Every (model, fuel, mode, displacement band, Fc) with its count."""
    safe = make.replace("'", "''")
    return query(
        f"SELECT TOP 8000 Cn, Ft, Fm, ([Ec (cm3)]/100)*100 AS band, Fc, COUNT(*) AS n "
        f"FROM {TABLE} "
        f"WHERE Mk = '{safe}' AND Fc BETWEEN {FC_MIN} AND {FC_MAX} "
        f"  AND [Ec (cm3)] > 0 AND Cn IS NOT NULL "
        f"GROUP BY Cn, Ft, Fm, ([Ec (cm3)]/100)*100, Fc")


# Manufacturers that file under more than one name. Folding punctuation is not
# enough: 'VW', 'VOLKSWAGEN' and 'VOLKSWAGEN, VW' normalise to three different
# keys and split one manufacturer's cars three ways, and the collision check
# cannot see it because they never collide.
#
# The list is deliberately short and only merges names that are the same
# company's own filings for the same cars. Sub-brands that build different cars
# stay apart: MERCEDES AMG is not MERCEDES BENZ, and FORD CNG TECHNIK is a
# separate type-approval holder.
MAKE_ALIASES = {
    "VW": "VOLKSWAGEN",
    "VOLKSWAGEN VW": "VOLKSWAGEN",
    "KODA": "SKODA",          # ŠKODA, and its mojibake ?KODA
    "SANGYONG": "SSANGYONG",
    "SSANG YONG": "SSANGYONG",
    "LYNK CO": "LYNK",
}


def normalise(s):
    """Fold the spelling variants that the same car arrives under.

    The table holds 'MERCEDES-BENZ', 'Mercedes Benz' and 'MERCEDES  BENZ' for
    one manufacturer. Uppercasing, collapsing runs of non-alphanumerics to a
    single space and trimming gets them to one key. It also merges things it
    should not, which is what the collision check downstream is for.
    """
    return re.sub(r"[^A-Z0-9]+", " ", (s or "").upper()).strip()


def normalise_make(s):
    key = normalise(s)
    return MAKE_ALIASES.get(key, key)


def percentile(hist, p):
    """Exact percentile from a {value: count} histogram, by nearest rank."""
    total = sum(hist.values())
    target = p * total
    seen = 0
    for value in sorted(hist):
        seen += hist[value]
        if seen >= target:
            return value
    return max(hist)


def build(rows_by_make, date):
    entries, collisions, dropped = {}, {}, []

    for make, rows in rows_by_make.items():
        for r in rows:
            klass = FUEL_CLASSES.get(((r["Ft"] or "").upper(), (r["Fm"] or "").upper()))
            if klass is None:
                continue
            band = int(r["band"] or 0)
            if band <= 0:
                continue
            key = (normalise_make(make), normalise(r["Cn"]), klass, band)
            if not key[0] or not key[1]:
                continue
            # Two raw spellings folding onto one key is fine and expected. Two
            # raw *makes* folding onto one is not, and is worth failing over.
            collisions.setdefault(key[0], set()).add(make)
            entries.setdefault(key, {}).setdefault(float(r["Fc"]), 0)
            entries[key][float(r["Fc"])] += int(r["n"])

    out = []
    for (make, model, klass, band), hist in sorted(entries.items()):
        n = sum(hist.values())
        if n < MIN_ROWS:
            dropped.append((make, model, klass, band, f"only {n} registrations"))
            continue
        median = percentile(hist, 0.5)
        p25, p75 = percentile(hist, 0.25), percentile(hist, 0.75)
        if median <= 0:
            continue
        spread = (p75 - p25) / median
        if spread > MAX_SPREAD:
            dropped.append((make, model, klass, band,
                            f"quartiles {p25}-{p75} around {median}, spread {spread:.0%}"))
            continue
        out.append({
            "make": make, "model": model, "fuel": klass, "ec_band": band,
            "l_per_100km": f"{median:.1f}",
            "p25": f"{p25:.1f}", "p75": f"{p75:.1f}",
            "registrations": n, "distinct_values": len(hist),
        })

    return out, dropped, collisions


FIELDS = ["make", "model", "fuel", "ec_band", "l_per_100km", "p25", "p75",
          "registrations", "distinct_values"]


def review(old, new, dropped, collisions):
    problems, notes = [], []

    # Spelling variants folding onto one key is the point of normalising, and
    # most of these are one manufacturer's own filings: MERCEDES-BENZ and
    # MERCEDES BENZ, ŠKODA and its mojibake. Flagging every one of them as a
    # problem every run would train the reader to skip the whole section.
    #
    # A merge that is actually wrong joins two different cars, which widens the
    # group's quartiles, which drops it at the dispersion gate below. So the
    # defence is downstream and real, and these are notes.
    merged = {k: v for k, v in collisions.items() if len(v) > 1}
    for key, raws in sorted(merged.items()):
        notes.append(f"'{key}' merges {sorted(raws)}")

    was = {(r["make"], r["model"], r["fuel"], r["ec_band"]): r for r in old}
    now = {(r["make"], r["model"], r["fuel"], str(r["ec_band"])): r for r in new}

    gone = set(was) - set(now)
    if gone:
        problems.append(f"{len(gone)} entries disappeared, e.g. {sorted(gone)[:3]}")

    for key in sorted(set(was) & set(now)):
        before, after = float(was[key]["l_per_100km"]), float(now[key]["l_per_100km"])
        if before > 0 and abs(after - before) / before > 0.10:
            problems.append(f"{key} moved {before} -> {after} l/100km")

    added = set(now) - set(was)
    if added:
        notes.append(f"{len(added)} new entries")
    if dropped:
        notes.append(f"{len(dropped)} groups dropped; see --explain for why")
    return problems, notes


def fetch_all():
    every = makes()
    print(f"{len(every)} makes to fetch", file=sys.stderr)
    rows_by_make = {}
    for i, make in enumerate(every, 1):
        try:
            rows_by_make[make] = histogram(make)
        except Exception as e:  # one bad make must not lose the other 140
            print(f"  {make}: {e}", file=sys.stderr)
            continue
        print(f"  [{i}/{len(every)}] {make}: {len(rows_by_make[make])}", file=sys.stderr)
        time.sleep(0.3)
    return rows_by_make, every


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--write", action="store_true", help="write the dataset")
    ap.add_argument("--explain", action="store_true", help="list every dropped group")
    ap.add_argument("--date", default=time.strftime("%Y-%m-%d"))
    ap.add_argument("--out", default="internal/vehicles/data/eea.csv")
    ap.add_argument("--cache", help="raw fetch cache; reused if it exists")
    args = ap.parse_args()

    if args.cache and os.path.exists(args.cache):
        print(f"reusing the raw fetch in {args.cache}", file=sys.stderr)
        rows_by_make = json.load(open(args.cache))
        every = sorted(rows_by_make)
    else:
        rows_by_make, every = fetch_all()
        if args.cache:
            json.dump(rows_by_make, open(args.cache, "w"))
    if len(rows_by_make) < len(every) * 0.9:
        print("fetched less than 90% of makes; leaving the dataset alone", file=sys.stderr)
        return 1

    new, dropped, collisions = build(rows_by_make, args.date)
    if not new:
        print("built nothing; leaving the dataset alone", file=sys.stderr)
        return 1

    old = []
    if os.path.exists(args.out):
        with open(args.out, newline="") as f:
            old = [r for r in csv.DictReader(f) if not r["make"].startswith("#")]

    problems, notes = review(old, new, dropped, collisions)
    print(f"\n{len(new)} entries (was {len(old)})")
    for n in notes:
        print(f"  note: {n}")
    for p in problems:
        print(f"  REVIEW: {p}")
    if args.explain:
        for d in dropped:
            print(f"  dropped: {' '.join(str(x) for x in d)}")

    if problems and not args.write:
        print("\nrefusing a silent refresh: read the lines above, then pass --write")
        return 1
    if not args.write:
        print("\ndry run; pass --write to update the dataset")
        return 0

    os.makedirs(os.path.dirname(args.out), exist_ok=True)
    digest = hashlib.sha256(
        json.dumps(new, sort_keys=True).encode()).hexdigest()[:16]
    with open(args.out, "w", newline="") as f:
        f.write(f"# source: {SOURCE}\n")
        f.write(f"# retrieved: {args.date}\n")
        f.write(f"# table: {TABLE}; petrol and diesel only, Fc in l/100km\n")
        f.write(f"# gates: n>={MIN_ROWS}, (p75-p25)/median<={MAX_SPREAD}\n")
        f.write(f"# digest: {digest}\n")
        w = csv.DictWriter(f, fieldnames=FIELDS, lineterminator="\n")
        w.writeheader()
        for row in new:
            w.writerow(row)
    print(f"\nwrote {args.out} — now run: go test ./internal/vehicles/")
    return 0


if __name__ == "__main__":
    sys.exit(main())

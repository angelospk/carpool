#!/usr/bin/env bash
# A world you can sign into and click around in.
#
# scripts/e2e.sh proves the system works and then leaves nothing behind, because
# it makes up phone numbers and does not tell you what they were. This makes a
# fixed cast with published routes, prints the numbers, and can be re-run.
#
#   DATABASE_URL=... JWT_SECRET=... DEV_MODE=true ./bin/api &
#   API=http://192.168.0.154:8080/v1 scripts/seed-demo.sh
#
# Seeding an API started with `DB=qa scripts/dev-lan.sh`? Pass the same DB=qa
# here (or DATABASE_URL), or the retiring below touches the lan database.
#
# DEV_MODE is required: it is what returns the OTP in the response instead of
# sending an SMS, so this script can sign the cast in without a phone.
set -euo pipefail

API=${API:-http://localhost:8080/v1}

j() { node -pe "const d=JSON.parse(require('fs').readFileSync(0));$1"; }
step() { printf '\n\033[1m%s\033[0m\n' "$*"; }

# Φέρες, out of the embedded settlement list. Signing up names a town and the
# server reads the prefecture off it, so nothing here needs a region id.
HOME_PLACE=${HOME_PLACE:-node/139260653}

# Everybody this makes is an ordinary adult. The age gate refuses anybody
# under eighteen and is tested where it lives, not here.
DOB=${DOB:-1990-01-01}

# Signing in is idempotent: the same number signs in as the same person, so
# re-running this tops the world up rather than duplicating it.
signin() {
  local phone=$1 name=$2 body code
  body=$(curl -s -X POST "$API/auth/otp/request" -H 'Content-Type: application/json' \
    -d "{\"phone\":\"$phone\"}")
  code=$(printf '%s' "$body" | j "d.dev_code || ''")
  if [ -z "$code" ]; then
    # Three codes per number per fifteen minutes, which a few re-runs of this
    # script will reach. Saying which of the two it is saves guessing.
    case "$body" in
      *rate_limited*) echo "$phone is rate limited: 3 codes per 15 minutes. Wait, or use other numbers." >&2 ;;
      *)              echo "no dev_code for $phone — is the API running with DEV_MODE=true? ($body)" >&2 ;;
    esac
    exit 1
  fi
  local verified ticket
  verified=$(curl -s -X POST "$API/auth/otp/verify" -H 'Content-Type: application/json' \
    -d "{\"phone\":\"$phone\",\"code\":\"$code\"}")
  ticket=$(printf '%s' "$verified" | j "d.signup_token || ''")
  if [ -z "$ticket" ]; then
    # Already an account. Signing in again is the whole point of this being
    # idempotent, so this is the ordinary path on a re-run.
    printf '%s' "$verified" | j "d.access_token"
    return
  fi
  curl -s -X POST "$API/auth/signup/complete" -H 'Content-Type: application/json' \
    -d "{\"signup_token\":\"$ticket\",\"name\":\"$name\",\"home_place_id\":\"$HOME_PLACE\",\"date_of_birth\":\"$DOB\"}" \
    | j "d.access_token"
}

# Tomorrow, in Athens. Anchored to one calendar date because the schedule is
# published in local wall time while the matcher works in instants, and deriving
# the weekday separately puts them on different days either side of midnight.
DAY=$(TZ=Europe/Athens date -d tomorrow +%Y-%m-%d)
DOW=$(TZ=Europe/Athens date -d "$DAY" +%u)

step "signing in the cast"
DRIVER_A=$(signin "+306900000101" "Γιώργος οδηγός")
DRIVER_B=$(signin "+306900000102" "Μαρία οδηγός")
PASSENGER=$(signin "+306900000103" "Νίκος επιβάτης")
echo "   three accounts"

step "giving the drivers cars"
# Through the API, so the consumption is worked out the way it is for a real
# driver rather than inserted by hand. The Yaris is in the dataset; the Corolla
# is not, and falls back to the CO2 on its registration document.
car() {
  curl -s -X PUT "$API/me/driver" -H "Authorization: Bearer $1" -H 'Content-Type: application/json' \
    -d "$2" | j "d.consumption_source + ' ' + d.fuel_consumption_l_100km.toFixed(1) + ' l/100km'"
}
echo "   Γιώργος: $(car "$DRIVER_A" '{"make":"Toyota","model":"Yaris","fuel":"petrol_hybrid","engine_cc":1490,"plate":"ΑΒΓ-1234","colour":"λευκό"}')"
echo "   Μαρία:   $(car "$DRIVER_B" '{"make":"Toyota","model":"Corolla Verso","fuel":"diesel","engine_cc":2000,"co2_g_km":160,"plate":"ΑΒΕ-5678","colour":"γκρι"}')"

step "publishing routes for $DAY (weekday $DOW)"
# Signing in is idempotent; publishing is not. Three runs of this script during
# one debugging session is how a demo account ends up showing the same 07:30
# departure three times over, which reads as a bug in the app and cost an
# evening to tell apart from one.
#
# There is no endpoint that lists a driver's own routes yet, so the demo cast's
# existing ones are retired directly. Scoped to the three seeded phone numbers,
# and skipped with a warning when the database is not reachable, because a
# script that silently duplicates is worse than one that says it cannot help.
DSN=${DATABASE_URL:-postgres://carpool:carpool@localhost:5433/${DB:-lan}?sslmode=disable}
# Named, never printed whole: a DSN can carry a password.
if [[ -n ${DATABASE_URL:-} ]]; then echo "   database: from DATABASE_URL"; else echo "   database: ${DB:-lan}"; fi
if psql "$DSN" -tAc 'SELECT 1' >/dev/null 2>&1; then
  psql "$DSN" -qtAc "
    UPDATE route_templates SET active = false
     WHERE active
       AND driver_id IN (SELECT id FROM users
                          WHERE phone IN ('+306900000101','+306900000102','+306900000103'))" >/dev/null
  echo "   retired the cast's previous routes"
else
  echo "   NOTE: no database at \$DATABASE_URL, so re-running will duplicate routes" >&2
fi

route() {
  curl -s -X POST "$API/routes" -H "Authorization: Bearer $1" -H 'Content-Type: application/json' \
    -d "$2" | j "d.template_id ? 'ok' : JSON.stringify(d)"
}
echo "   Αλεξανδρούπολη → Κομοτηνή:  $(route "$DRIVER_A" "{
  \"origin_lon\": 25.8743, \"origin_lat\": 40.8476, \"origin_label\": \"Αλεξανδρούπολη\",
  \"destination_lon\": 25.4048, \"destination_lat\": 41.1224, \"destination_label\": \"Κομοτηνή\",
  \"kind\": \"recurring\", \"weekdays\": [$DOW],
  \"depart_earliest\": \"07:30\", \"depart_latest\": \"08:00\",
  \"seats\": 3, \"pricing_mode\": \"formula\"}")"
# Deliberately Αλεξανδρούπολη → Ορεστιάδα and not the reverse. The northbound
# road route is simple; the southbound one is not, and route_templates_geom_simple
# rejects it. See RISKS.md — it is a real limitation, not a quirk of this script.
echo "   Αλεξανδρούπολη → Ορεστιάδα: $(route "$DRIVER_B" "{
  \"origin_lon\": 25.8743, \"origin_lat\": 40.8476, \"origin_label\": \"Αλεξανδρούπολη\",
  \"destination_lon\": 26.5297, \"destination_lat\": 41.5039, \"destination_label\": \"Ορεστιάδα\",
  \"kind\": \"recurring\", \"weekdays\": [$DOW],
  \"depart_earliest\": \"06:45\", \"depart_latest\": \"07:15\",
  \"seats\": 2, \"pricing_mode\": \"formula\"}")"

cat <<TXT

$(printf '\033[1mSign in with any of these.\033[0m') The code is filled in for you
on a DEV_MODE server, so just tap through.

  +306900000101   Γιώργος   driver, Αλεξανδρούπολη → Κομοτηνή, 3 seats
  +306900000102   Μαρία     driver, Αλεξανδρούπολη → Ορεστιάδα, 2 seats
  +306900000103   Νίκος     passenger, no car

As Νίκος, ask for a ride along one of those corridors on $DAY and one of the
drivers gets asked. Pickup points are typed as "lon, lat Label" until the
picker exists — for the first route, try:

  25.8743, 40.8476 Αλεξανδρούπολη
  25.4048, 41.1224 Κομοτηνή

TXT

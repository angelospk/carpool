#!/usr/bin/env bash
# End-to-end scenario against a running stack, exercising the real HTTP API,
# real PostGIS and real OSRM. Nothing here is mocked.
#
#   docker compose -f docker-compose.dev.yml up -d
#   DATABASE_URL=... JWT_SECRET=... DEV_MODE=true ./bin/api &
#   scripts/e2e.sh
set -euo pipefail

API=${API:-http://localhost:8099/v1}
LOG=${LOG:-/tmp/api-e2e.log}
DSN=${DSN:-postgres://carpool:carpool@localhost:5433/smoke?sslmode=disable}

j() { node -pe "const d=JSON.parse(require('fs').readFileSync(0));$1"; }
step() { printf '\n\033[1m%s\033[0m\n' "$*"; }

REGION=$(curl -s "$API/regions" | j "d.regions[0].id")

# DEV_MODE prints the OTP to the log instead of sending an SMS.
signup() {
  curl -s -X POST "$API/auth/otp/request" -H 'Content-Type: application/json' -d "{\"phone\":\"$1\"}" -o /dev/null
  local code
  code=$(grep -o '"message":"[0-9]\{6\}' "$LOG" | tail -1 | grep -o '[0-9]\{6\}')
  curl -s -X POST "$API/auth/otp/verify" -H 'Content-Type: application/json' \
    -d "{\"phone\":\"$1\",\"code\":\"$code\",\"name\":\"$2\",\"region_id\":\"$REGION\"}" | j "d.access_token"
}

step "1. sign up a driver and a passenger by phone OTP"
DRIVER=$(signup "+306900000021" "Οδηγός")
PASSENGER=$(signup "+306900000022" "Επιβάτης")
[ -n "$DRIVER" ] && [ -n "$PASSENGER" ] && echo "   both authenticated"

step "2. give the driver a car profile"
psql "$DSN" -q -c "INSERT INTO driver_profiles (user_id, car_model, car_plate, detour_budget_min)
                   SELECT id,'Toyota Yaris','ABC-1234',20 FROM users WHERE phone='+306900000021'
                   ON CONFLICT (user_id) DO UPDATE SET detour_budget_min = 20"

step "3. publish a recurring route, snapped by real OSRM"
DOW=$(date -d tomorrow +%u)
curl -s -X POST "$API/routes" -H "Authorization: Bearer $DRIVER" -H 'Content-Type: application/json' -d "{
  \"origin_lon\":25.8743,\"origin_lat\":40.8476,
  \"destination_lon\":26.1720,\"destination_lat\":40.8960,
  \"origin_label\":\"Αλεξανδρούπολη\",\"destination_label\":\"Φέρες\",
  \"seats\":3,\"kind\":\"recurring\",\"weekdays\":[$DOW],
  \"depart_earliest\":\"07:30\",\"depart_latest\":\"08:30\"}" | j "'   route_km=' + d.route_km"

step "4. occurrences materialized over the rolling horizon"
psql "$DSN" -tAc "SELECT '   ' || count(*) || ' occurrences' FROM trip_occurrences WHERE status='scheduled'"

step "5. passenger posts a request; the backend asks the best driver"
WS=$(date -u -d 'tomorrow 04:00' +%Y-%m-%dT%H:%M:%SZ)
WE=$(date -u -d 'tomorrow 06:30' +%Y-%m-%dT%H:%M:%SZ)
REQ=$(curl -s -X POST "$API/requests" -H "Authorization: Bearer $PASSENGER" -H 'Content-Type: application/json' -d "{
  \"origin_lon\":25.9200,\"origin_lat\":40.8600,
  \"destination_lon\":26.1400,\"destination_lat\":40.8900,
  \"origin_label\":\"Άνθεια\",\"destination_label\":\"Φέρες\",
  \"window_start\":\"$WS\",\"window_end\":\"$WE\",\"seats\":1}")
ASK=$(echo "$REQ" | j "d.ask_id || ''")
[ -n "$ASK" ] || { echo "   no driver was asked"; exit 1; }
echo "   ask $ASK"

step "6. driver accepts"
curl -s -X POST "$API/asks/$ASK/accept" -H "Authorization: Bearer $DRIVER" | j "'   booking ' + d.booking_id"

step "7. resulting state"
psql "$DSN" -tAc "
  SELECT '   booking=' || b.status
      || ' price=' || b.price_eur || ' EUR'
      || ' seats_free=' || o.seats_free
      || ' detour=' || a.detour_min || ' min / ' || a.detour_km || ' km'
  FROM bookings b
  JOIN trip_occurrences o ON o.id = b.occurrence_id
  JOIN asks a ON a.id = b.ask_id"

printf '\n\033[32me2e scenario passed\033[0m\n'

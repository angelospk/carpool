#!/usr/bin/env bash
# QA scenario: a driver calls off a departure somebody had a seat on.
#
# scripts/e2e.sh walks the happy path. This walks the one that goes wrong: the
# recovery of a stranded passenger, which happens after the cancelling
# transaction rather than inside it, and so depends on the outbox dispatcher
# actually running and actually being able to read what was queued for it.
#
# Step 6 is the one that found something: a cancellation notice nobody could
# deliver had been dead-lettering silently for months, and no unit test looked
# at the far end of the queue.
#
#   docker compose -f docker-compose.dev.yml up -d
#   DATABASE_URL=... JWT_SECRET=... DEV_MODE=true ./bin/api &
#   scripts/qa-stranded.sh
#
# RUN distinguishes one run from the next in the phone numbers it signs up, so
# it can be run repeatedly against the same database.
set -euo pipefail

API=${API:-http://localhost:8099/v1}
LOG=${LOG:-/tmp/api-e2e.log}
DSN=${DSN:-postgres://carpool:carpool@localhost:5433/smoke?sslmode=disable}

j() { node -pe "const d=JSON.parse(require('fs').readFileSync(0));$1"; }
step() { printf '\n\033[1m%s\033[0m\n' "$*"; }
fail() { printf '\033[31m   FAIL: %s\033[0m\n' "$*"; exit 1; }

RUN=${RUN:-04}
HOME_PLACE=node/139260653
DOB=1990-01-01

signup_pair() {
  curl -s -X POST "$API/auth/otp/request" -H 'Content-Type: application/json' -d "{\"phone\":\"$1\"}" -o /dev/null
  local code verified ticket
  code=$(grep -ao '"message":"[0-9]\{6\}' "$LOG" | tail -1 | grep -o '[0-9]\{6\}')
  verified=$(curl -s -X POST "$API/auth/otp/verify" -H 'Content-Type: application/json' \
    -d "{\"phone\":\"$1\",\"code\":\"$code\"}")
  ticket=$(printf '%s' "$verified" | j "d.signup_token || ''")
  if [ -z "$ticket" ]; then printf '%s' "$verified"; return; fi
  curl -s -X POST "$API/auth/signup/complete" -H 'Content-Type: application/json' \
    -d "{\"signup_token\":\"$ticket\",\"name\":\"$2\",\"home_place_id\":\"$HOME_PLACE\",\"date_of_birth\":\"$DOB\"}"
}
signup() { signup_pair "$1" "$2" | j "d.access_token"; }

become_driver() { # token phone plate
  curl -s -X POST "$API/me/vehicles" -H "Authorization: Bearer $1" -H 'Content-Type: application/json' \
    -d "{\"make\":\"Toyota\",\"model\":\"Yaris\",\"fuel\":\"petrol\",\"engine_cc\":1330,
         \"colour\":\"ασημί\",\"plate\":\"$3\",\"label\":\"το αμάξι\"}" > /dev/null
  psql "$DSN" -q -c "UPDATE driver_profiles SET detour_budget_min = 20
                     WHERE user_id = (SELECT id FROM users WHERE phone='$2')"
}

DAY=$(TZ=Europe/Athens date -d tomorrow +%Y-%m-%d)
DOW=$(TZ=Europe/Athens date -d "$DAY" +%u)
publish() { # token depart_earliest depart_latest
  curl -s -X POST "$API/routes" -H "Authorization: Bearer $1" -H 'Content-Type: application/json' -d "{
    \"origin_lon\":25.8743,\"origin_lat\":40.8476,
    \"destination_lon\":26.1720,\"destination_lat\":40.8960,
    \"origin_label\":\"Αλεξανδρούπολη\",\"destination_label\":\"Φέρες\",
    \"seats\":3,\"kind\":\"recurring\",\"weekdays\":[$DOW],
    \"depart_earliest\":\"$2\",\"depart_latest\":\"$3\"}" | j "d.route_km"
}

# Counted before anything happens rather than at the end: the script is made to
# be re-run against the same database, and a message another run gave up on is
# not this run's news.
DEAD_BEFORE=$(psql "$DSN" -tAc "SELECT count(*) FROM outbox WHERE dead_at IS NOT NULL")

step "1. two drivers on the same corridor, and a passenger"
A=$(signup "+3069000${RUN}041" "Οδηγός Α")
B=$(signup "+3069000${RUN}042" "Οδηγός Β")
P=$(signup "+3069000${RUN}043" "Επιβάτης")
become_driver "$A" "+3069000${RUN}041" "AA${RUN}-1111"
become_driver "$B" "+3069000${RUN}042" "BB${RUN}-2222"
# Α leaves earlier, so the matcher prefers her and Β is the fallback.
echo "   Α route_km=$(publish "$A" "07:30" "08:30")"
echo "   Β route_km=$(publish "$B" "07:45" "08:45")"

step "2. the passenger asks, and is given a seat"
WS=$(date -u -d "TZ=\"Europe/Athens\" $DAY 06:00" +%Y-%m-%dT%H:%M:%SZ)
WE=$(date -u -d "TZ=\"Europe/Athens\" $DAY 10:00" +%Y-%m-%dT%H:%M:%SZ)
REQ=$(curl -s -X POST "$API/requests" -H "Authorization: Bearer $P" -H 'Content-Type: application/json' -d "{
  \"origin_lon\":25.9200,\"origin_lat\":40.8600,
  \"destination_lon\":26.1400,\"destination_lat\":40.8900,
  \"origin_label\":\"Άνθεια\",\"destination_label\":\"Φέρες\",
  \"window_start\":\"$WS\",\"window_end\":\"$WE\",\"seats\":1}")
REQ_ID=$(echo "$REQ" | j "d.request_id")
ASK=$(echo "$REQ" | j "d.ask_id || ''")
[ -n "$ASK" ] || fail "nobody was asked"
ASKED=$(psql "$DSN" -tAc "SELECT u.phone FROM asks a
                          JOIN trip_occurrences o ON o.id = a.occurrence_id
                          JOIN route_templates t ON t.id = o.template_id
                          JOIN users u ON u.id = t.driver_id WHERE a.id='$ASK'")
echo "   asked $ASKED"
WHO=$A; [ "$ASKED" = "+3069000${RUN}042" ] && WHO=$B
BOOKING=$(curl -s -X POST "$API/asks/$ASK/accept" -H "Authorization: Bearer $WHO" | j "d.booking_id")
[ -n "$BOOKING" ] || fail "the accept produced no booking"
echo "   booking $BOOKING"

TRIP=$(psql "$DSN" -tAc "SELECT occurrence_id FROM bookings WHERE id='$BOOKING'")
SEEN=$(curl -s "$API/trips/upcoming" -H "Authorization: Bearer $P" | j "d.trips.length")
[ "$SEEN" = "1" ] || fail "the passenger does not see the trip they booked ($SEEN)"
echo "   the passenger sees 1 upcoming trip"

step "3. the driver calls the departure off"
CODE=$(curl -s -o /tmp/qa-cancel.json -w '%{http_code}' -X DELETE "$API/trips/$TRIP" -H "Authorization: Bearer $WHO")
echo "   cancel: $CODE $(cat /tmp/qa-cancel.json)"
[ "$CODE" = "204" ] || [ "$CODE" = "200" ] || fail "the driver could not call off their own trip"

step "4. the passenger is not left holding a seat that does not exist"
BSTATUS=$(psql "$DSN" -tAc "SELECT status FROM bookings WHERE id='$BOOKING'")
[ "$BSTATUS" = "cancelled_by_driver" ] || fail "the booking is $BSTATUS, not cancelled by the driver"
SEEN=$(curl -s "$API/trips/upcoming" -H "Authorization: Bearer $P" | j "d.trips.length")
[ "$SEEN" = "0" ] || fail "the cancelled trip is still on the passenger's screen ($SEEN)"
echo "   booking cancelled, and off their screen"

step "5. the outbox carries the recovery: the other driver is asked, unprompted"
for _ in $(seq 40); do
  N=$(psql "$DSN" -tAc "SELECT count(*) FROM asks WHERE request_id='$REQ_ID' AND id <> '$ASK'")
  [ "$N" != "0" ] && break
  sleep 1
done
[ "$N" != "0" ] || fail "nobody was asked again; the passenger is stranded"
psql "$DSN" -tAc "
  SELECT '   asked ' || u.name || ', request is now ' || r.status
  FROM asks a
  JOIN trip_occurrences o ON o.id = a.occurrence_id
  JOIN route_templates t ON t.id = o.template_id
  JOIN users u ON u.id = t.driver_id
  JOIN ride_requests r ON r.id = a.request_id
  WHERE a.request_id='$REQ_ID' AND a.id <> '$ASK'"

step "6. and the outbox did not dead-letter anything doing it"
DEAD=$(psql "$DSN" -tAc "SELECT count(*) FROM outbox WHERE dead_at IS NOT NULL")
[ "$DEAD" = "$DEAD_BEFORE" ] || {
  psql "$DSN" -c "SELECT kind, last_error, payload FROM outbox WHERE dead_at IS NOT NULL"
  fail "$((DEAD - DEAD_BEFORE)) messages were dead-lettered by this run"
}
psql "$DSN" -tAc "SELECT '   ' || kind || ': ' || count(*) || ' delivered' FROM outbox WHERE done_at IS NOT NULL GROUP BY kind ORDER BY kind"

printf '\n\033[32mstranded-passenger recovery passed\033[0m\n'

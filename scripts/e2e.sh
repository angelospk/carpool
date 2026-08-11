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

# Φέρες, out of the embedded settlement list. The server reads the prefecture
# off it, so nothing here has to know what a region id is.
HOME_PLACE=${HOME_PLACE:-node/139260653}

# Everybody this makes is an ordinary adult. The age gate refuses anybody
# under eighteen and is tested where it lives, not here.
DOB=${DOB:-1990-01-01}

# DEV_MODE prints the OTP to the log instead of sending an SMS.
#
# Verifying answers one of two ways: tokens for a number that is already an
# account, or a ticket for one that is not. A fresh smoke database is always the
# second, but handling both keeps this honest against a re-run.
signup_pair() {
  curl -s -X POST "$API/auth/otp/request" -H 'Content-Type: application/json' -d "{\"phone\":\"$1\"}" -o /dev/null
  local code verified ticket
  code=$(grep -ao '"message":"[0-9]\{6\}' "$LOG" | tail -1 | grep -o '[0-9]\{6\}')
  verified=$(curl -s -X POST "$API/auth/otp/verify" -H 'Content-Type: application/json' \
    -d "{\"phone\":\"$1\",\"code\":\"$code\"}")
  ticket=$(printf '%s' "$verified" | j "d.signup_token || ''")
  if [ -z "$ticket" ]; then
    printf '%s' "$verified"
    return
  fi
  curl -s -X POST "$API/auth/signup/complete" -H 'Content-Type: application/json' \
    -d "{\"signup_token\":\"$ticket\",\"name\":\"$2\",\"home_place_id\":\"$HOME_PLACE\",\"date_of_birth\":\"$DOB\"}"
}

signup() { signup_pair "$1" "$2" | j "d.access_token"; }

step "1. sign up a driver and a passenger by phone OTP"
DRIVER=$(signup "+306900000021" "Οδηγός")
PASSENGER=$(signup "+306900000022" "Επιβάτης")
[ -n "$DRIVER" ] && [ -n "$PASSENGER" ] && echo "   both authenticated"

step "2. the driver says what they drive"
# Through the real endpoint rather than an INSERT, because a route cannot be
# published without a car and the consumption behind every suggested
# contribution is derived from these fields rather than typed.
curl -s -X POST "$API/me/vehicles" -H "Authorization: Bearer $DRIVER" -H 'Content-Type: application/json' -d '{
  "make":"Toyota","model":"Yaris","fuel":"petrol","engine_cc":1330,
  "colour":"ασημί","plate":"ABC-1234","label":"το αμάξι"}' | j "'   ' + d.make + ' ' + d.model + ', ' + d.consumption_l_100km + ' l/100km (' + d.consumption_source + ')'"
# Declaring the car is the whole of becoming a driver. Nothing here reaches past
# the API to arrange that, which is the point: it used to, and that is how the
# app came to have no way of doing it at all.
psql "$DSN" -q -c "UPDATE driver_profiles SET detour_budget_min = 20
                   WHERE user_id = (SELECT id FROM users WHERE phone='+306900000021')"

step "3. publish a recurring route, snapped by real OSRM"
# Everything here is anchored to one Athens calendar date. The schedule is
# published in Athens wall time and the request window is sent as instants, so
# deriving the weekday locally and the window in UTC put them on different days
# whenever the script ran between local midnight and UTC midnight.
DAY=$(TZ=Europe/Athens date -d tomorrow +%Y-%m-%d)
DOW=$(TZ=Europe/Athens date -d "$DAY" +%u)
curl -s -X POST "$API/routes" -H "Authorization: Bearer $DRIVER" -H 'Content-Type: application/json' -d "{
  \"origin_lon\":25.8743,\"origin_lat\":40.8476,
  \"destination_lon\":26.1720,\"destination_lat\":40.8960,
  \"origin_label\":\"Αλεξανδρούπολη\",\"destination_label\":\"Φέρες\",
  \"seats\":3,\"kind\":\"recurring\",\"weekdays\":[$DOW],
  \"depart_earliest\":\"07:30\",\"depart_latest\":\"08:30\"}" | j "'   route_km=' + d.route_km"

step "4. occurrences materialized over the rolling horizon"
psql "$DSN" -tAc "SELECT '   ' || count(*) || ' occurrences' FROM trip_occurrences WHERE status='scheduled'"

step "5. passenger posts a request; the backend asks the best driver"
# The same Athens day, 06:00 to 10:00 Athens time, which brackets the 07:30
# departure whatever the offset happens to be that month.
WS=$(date -u -d "TZ=\"Europe/Athens\" $DAY 06:00" +%Y-%m-%dT%H:%M:%SZ)
WE=$(date -u -d "TZ=\"Europe/Athens\" $DAY 10:00" +%Y-%m-%dT%H:%M:%SZ)
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

step "8. the trip is visible to both sides"
TRIP=$(curl -s "$API/trips/upcoming" -H "Authorization: Bearer $DRIVER" | j "d.trips[0].id")
for who in DRIVER PASSENGER; do
  token=${!who}
  role=$(curl -s "$API/trips/upcoming" -H "Authorization: Bearer $token" | j "d.trips[0].role")
  echo "   $who sees the trip as: $role"
done
echo "   plate revealed to the passenger: $(curl -s "$API/trips/$TRIP" -H "Authorization: Bearer $PASSENGER" | j "d.driver.car_plate")"

step "9. they exchange a message"
curl -s -X POST "$API/trips/$TRIP/messages" -H "Authorization: Bearer $PASSENGER" \
  -H 'Content-Type: application/json' -d '{"body":"Θα είμαι στη στάση στις 07:35"}' \
  | j "'   passenger: ' + d.body"
curl -s "$API/trips/$TRIP/messages" -H "Authorization: Bearer $DRIVER" \
  | j "'   driver reads ' + d.messages.length + ' message(s)'"

step "10. the ride happens and both sides confirm"
# The trip is scheduled for tomorrow; move it into the past so it can be
# confirmed, which is what the real passage of time would do.
psql "$DSN" -q -c "UPDATE trip_occurrences SET depart_earliest = now() - interval '2 hours',
                                               depart_latest   = now() - interval '90 minutes'"
curl -s -o /dev/null -w '   driver confirm: %{http_code}\n'    -X POST "$API/trips/$TRIP/confirm" -H "Authorization: Bearer $DRIVER"
curl -s -o /dev/null -w '   passenger confirm: %{http_code}\n' -X POST "$API/trips/$TRIP/confirm" -H "Authorization: Bearer $PASSENGER"
psql "$DSN" -tAc "SELECT '   booking is now ' || status FROM bookings"

step "11. they rate each other"
curl -s -o /dev/null -w '   passenger rates driver: %{http_code}\n' \
  -X POST "$API/trips/$TRIP/rate" -H "Authorization: Bearer $PASSENGER" \
  -H 'Content-Type: application/json' -d '{"stars":5,"tag":"punctual"}'
PAX=$(psql "$DSN" -tAc "SELECT passenger_id FROM bookings LIMIT 1")
curl -s -o /dev/null -w '   driver rates passenger: %{http_code}\n' \
  -X POST "$API/trips/$TRIP/rate" -H "Authorization: Bearer $DRIVER" \
  -H 'Content-Type: application/json' -d "{\"stars\":5,\"ratee_id\":\"$PAX\"}"
psql "$DSN" -tAc "SELECT '   ' || count(*) || ' ratings stored' FROM ratings"

step "12. somebody wants a journey nobody drives yet"
# The return leg. The published route runs Αλεξανδρούπολη → Φέρες, so a request
# going the other way along the same road is rejected on direction, which is
# what makes this a corridor with no driver rather than a near miss.
WAITER=$(signup "+306900000023" "Δεύτερη επιβάτις")
REQ2=$(curl -s -X POST "$API/requests" -H "Authorization: Bearer $WAITER" -H 'Content-Type: application/json' -d "{
  \"origin_lon\":26.1400,\"origin_lat\":40.8900,
  \"destination_lon\":25.9200,\"destination_lat\":40.8600,
  \"origin_label\":\"Φέρες\",\"destination_label\":\"Άνθεια\",
  \"window_start\":\"$WS\",\"window_end\":\"$WE\",\"seats\":1}")
REQ2_ID=$(echo "$REQ2" | j "d.request_id")
[ -z "$(echo "$REQ2" | j "d.ask_id || ''")" ] || { echo "   somebody was asked, so this proves nothing"; exit 1; }
echo "   request $REQ2_ID is open and nobody was asked"

step "13. the driver publishes that route; she is offered it without asking again"
# Nothing below touches the request. Publishing materializes the departure,
# which queues the question \"who has been waiting for this?\", and the outbox
# dispatcher carries it the rest of the way on its own five-second timer.
curl -s -X POST "$API/routes" -H "Authorization: Bearer $DRIVER" -H 'Content-Type: application/json' -d "{
  \"origin_lon\":26.1720,\"origin_lat\":40.8960,
  \"destination_lon\":25.8743,\"destination_lat\":40.8476,
  \"origin_label\":\"Φέρες\",\"destination_label\":\"Αλεξανδρούπολη\",
  \"seats\":3,\"kind\":\"recurring\",\"weekdays\":[$DOW],
  \"depart_earliest\":\"07:30\",\"depart_latest\":\"08:30\"}" | j "'   return route published, route_km=' + d.route_km"

for _ in $(seq 30); do
  OFFERED=$(psql "$DSN" -tAc "SELECT count(*) FROM asks WHERE request_id='$REQ2_ID'")
  [ "$OFFERED" != "0" ] && break
  sleep 1
done
[ "$OFFERED" != "0" ] || { echo "   she was never offered the trip"; exit 1; }
psql "$DSN" -tAc "
  SELECT '   offered ' || a.detour_min || ' min detour at ' || a.suggested_price_eur || ' EUR'
      || ', request is now ' || r.status
  FROM asks a JOIN ride_requests r ON r.id = a.request_id
  WHERE a.request_id='$REQ2_ID'"

step "14. a lost refresh response does not sign anyone out"
# The app persists a rotated token only after the response arrives. Retrying with
# the attempt id that spent the old token must return that same successor rather
# than looking like a replay and ending the session.
REFRESH=$(signup_pair "+306900000023" "Δοκιμή" | j "d.refresh_token")
ATTEMPT=$(uuidgen | tr 'A-Z' 'a-z')
rotate() {
  curl -s -X POST "$API/auth/refresh" -H 'Content-Type: application/json' \
    -d "{\"refresh_token\":\"$REFRESH\",\"attempt_id\":\"$1\"}"
}
FIRST=$(rotate "$ATTEMPT" | j "d.refresh_token")
RETRY=$(rotate "$ATTEMPT" | j "d.refresh_token")
[ "$FIRST" = "$RETRY" ] || { echo "   retry returned a different token"; exit 1; }
echo "   the retry got the same token back"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/auth/refresh" \
  -H 'Content-Type: application/json' -d "{\"refresh_token\":\"$RETRY\"}")
[ "$STATUS" = "200" ] || { echo "   the session did not survive the retry ($STATUS)"; exit 1; }
echo "   the session is still alive"

printf '\n\033[32me2e scenario passed\033[0m\n'

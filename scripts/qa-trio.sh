#!/usr/bin/env bash
# QA scenarios: one driver and two or three passengers, end to end, against a
# running API — specs/MATCHING.md, specs/DRIVING-NOW.md.
#
# Real clock, real OSRM, the outbox dispatcher and the wake jobs on their own
# schedule. One function per scenario; each prints ok / FAIL with its evidence
# and the script goes on to the next whatever happens. Exit 1 if any FAILed.
#
#   IP=127.0.0.1 API_PORT=8188 DB=qa1017api scripts/dev-lan.sh      (already running)
#   API=http://127.0.0.1:8188/v1 \
#   DSN=postgres://carpool:carpool@localhost:5433/qa1017api?sslmode=disable \
#     scripts/qa-trio.sh [scenario numbers...]
#
# The corridor is Αλεξανδρούπολη -> Κομοτηνή (E90, about 58 km) and the villages
# on it. Each scenario works on a day of its own, so cars left over from another
# scenario (or an earlier run) cannot be offered to its passengers.
#
#   1  a driver publishes, the first passenger is asked and accepted
#   2  a party of two from a village; the itinerary and who sees whom
#   3  a third passenger, the car full: nobody is asked, and what they are told
#   4  the first passenger cancels, the waiting one is woken and asked
#   5  the party cap: two parties aboard, a third refused
#   6  races: the last seat requested together, one ask accepted twice
#   7  declined and expired offers move on to the next driver
#   8  the driver calls off the departure with two aboard
#   9  the driver edits the route while one departure is booked
#  10  chat on the trip, and who is shut out
#  11  live location: before the window, in it, and who sees whom
#  12  the same journey on three days, one accepted
#  13  refusals at the door: seats, window, ends, luggage
#  14  the outbox and the seat counts after the run
#
# RUN (0..7) picks a block of 12 phone numbers, +3069000020NN, NN = RUN*12 + n.
# The accounts are reused from run to run — OTP codes are limited to three per
# number per quarter of an hour — so tokens are kept in $TOKDIR and every
# account is cleared of what a previous run left before the scenarios start (and
# again on the way out; KEEP=1 leaves the last state standing for looking at).
# Publishing and editing a route is limited to 20 an hour per account and a run
# uses about ten from the driver D: after two runs in an hour, change RUN.
#
# The only write this script makes to the database itself is scenario 7's
# expires_at on one ask: the offer lasts 30 minutes (asks.DefaultTTL) and the
# sweeper's work on a lapsed one is what is being looked at, not the waiting.
set -euo pipefail

API=${API:-http://127.0.0.1:8188/v1}
DSN=${DSN:-postgres://carpool:carpool@localhost:5433/qa1017api?sslmode=disable}
RUN=${RUN:-0}
TOKDIR=${TOKDIR:-/tmp/qa-trio-tokens}
HOME_PLACE=node/139260653
DOB=1990-01-01
TMP=$(mktemp -d /tmp/qa-trio.XXXXXX)
# What the run leaves published, asked and booked is taken down again on the way
# out (KEEP=1 leaves it for looking at): cars from one block of accounts must not
# be offered to the passengers of another.
finish() { [ -n "${KEEP:-}" ] || reset_all > /dev/null 2>&1 || true; rm -rf "$TMP"; }
trap finish EXIT
mkdir -p "$TOKDIR" "$TMP/state"

# Who is who: name, index in the block.
NAMES=(D D2 D3 P1 P2 P3 P4 P5 P6 X)
idx_of() { local i; for i in "${!NAMES[@]}"; do [ "${NAMES[$i]}" = "$1" ] && { echo "$i"; return; }; done; echo "no such account $1" >&2; return 1; }
phone_of() { case $1 in +*) echo "$1";; *) printf '+3069000020%02d' $((RUN * 12 + $(idx_of "$1")));; esac; }

j() { node -pe "const d=JSON.parse(require('fs').readFileSync(0));$1"; }
q() { psql "$DSN" -tAc "$1"; }
step() { printf '\n\033[1m%s\033[0m\n' "$*"; }
ok() { printf '   \033[32mok\033[0m %s\n' "$*"; }
# A failed check: said, remembered for the scenario's verdict, and the
# scenario goes on — the rest of it may still say something.
soft() { printf '\033[31m   FAIL: %s\033[0m\n' "$*"; echo "$*" >> "$TMP/softfail"; }
# A failed step the rest of the scenario depends on: said, and the scenario ends.
fail() { printf '\033[31m   FAIL: %s\033[0m\n' "$*"; echo "$*" >> "$TMP/softfail"; exit 1; }
# check "what" cond-result evidence: ok when cond-result is 0.
check() { if [ "$2" = 0 ]; then ok "$1 — $3"; else soft "$1 — $3"; fi; }
eq() { if [ "$2" = "$3" ]; then ok "$1: $2"; else soft "$1: got '$2', want '$3'"; fi; }
must_eq() { if [ "$2" = "$3" ]; then ok "$1: $2"; else fail "$1: got '$2', want '$3'"; fi; }
state_set() { printf '%s' "$2" > "$TMP/state/$1"; }
state_get() { cat "$TMP/state/$1" 2>/dev/null || true; }

# ---- accounts ----------------------------------------------------------------

otp_signin() { # name -> access token, refresh token saved
  local name=$1 phone ip body code verified ticket resp n
  phone=$(phone_of "$name")
  for n in 1 2 3; do
    ip=127.0.0.$((10 + RANDOM % 240)) # the sign-in limit is per source address
    body=$(curl -s --interface "$ip" -X POST "$API/auth/otp/request" -H 'Content-Type: application/json' -d "{\"phone\":\"$phone\"}")
    code=$(printf '%s' "$body" | j "d.dev_code || ''")
    [ -n "$code" ] && break
  done
  [ -n "$code" ] || { echo "no dev_code for $phone: $body" >&2; return 1; }
  verified=$(curl -s --interface "$ip" -X POST "$API/auth/otp/verify" -H 'Content-Type: application/json' \
    -d "{\"phone\":\"$phone\",\"code\":\"$code\"}")
  ticket=$(printf '%s' "$verified" | j "d.signup_token || ''")
  if [ -n "$ticket" ]; then
    verified=$(curl -s --interface "$ip" -X POST "$API/auth/signup/complete" -H 'Content-Type: application/json' \
      -d "{\"signup_token\":\"$ticket\",\"name\":\"QT $name\",\"home_place_id\":\"$HOME_PLACE\",\"date_of_birth\":\"$DOB\"}")
  fi
  save_tokens "$name" "$verified"
}
save_tokens() { # name json
  local acc ref
  acc=$(printf '%s' "$2" | j "d.access_token || ''")
  ref=$(printf '%s' "$2" | j "d.refresh_token || ''")
  [ -n "$acc" ] || { echo "no tokens for $1: $2" >&2; return 1; }
  printf '%s' "$acc" > "$TOKDIR/$(phone_of "$1").acc"
  printf '%s' "$ref" > "$TOKDIR/$(phone_of "$1").ref"
}
refresh_tokens() { # name
  local ref resp
  ref=$(cat "$TOKDIR/$(phone_of "$1").ref" 2>/dev/null) || return 1
  [ -n "$ref" ] || return 1
  resp=$(curl -s -X POST "$API/auth/refresh" -H 'Content-Type: application/json' -d "{\"refresh_token\":\"$ref\"}")
  save_tokens "$1" "$resp" 2>/dev/null
}
login() { refresh_tokens "$1" || otp_signin "$1"; }
tok() { cat "$TOKDIR/$(phone_of "$1").acc"; }

# call METHOD PATH NAME [BODY] -> HTTP code on stdout; body in ${OUT:-$TMP/body}.
call() {
  local out=${OUT:-$TMP/body} code
  code=$(curl -s -o "$out" -w '%{http_code}' -X "$1" "$API$2" -H "Authorization: Bearer $(tok "$3")" \
    -H 'Content-Type: application/json' ${4:+-d "$4"})
  if [ "$code" = 401 ] && refresh_tokens "$3"; then
    code=$(curl -s -o "$out" -w '%{http_code}' -X "$1" "$API$2" -H "Authorization: Bearer $(tok "$3")" \
      -H 'Content-Type: application/json' ${4:+-d "$4"})
  fi
  printf '%s' "$code"
}
body() { cat "${OUT:-$TMP/body}"; }
err_code() { body | j "d.error && d.error.code || d.code || ''" 2>/dev/null || true; }
uid_of() { q "SELECT id FROM users WHERE phone = '$(phone_of "$1")'"; }

vehicle_for() { # name -> a car, once
  local n
  n=$(call GET /me/vehicles "$1" >/dev/null; body | j "(d.vehicles || d).length")
  [ "$n" = 0 ] || return 0
  call POST /me/vehicles "$1" "{\"make\":\"Toyota\",\"model\":\"Yaris\",\"fuel\":\"petrol\",\"engine_cc\":1330,
    \"colour\":\"ασημί\",\"plate\":\"QT$(printf '%02d' $((RUN * 12 + $(idx_of "$1"))))-1000\",\"label\":\"το αμάξι\"}" > /dev/null
}

# Everything an earlier run (or an earlier scenario that died) left behind.
reset_account() { # name
  local id kind
  call GET /requests/mine "$1" > /dev/null
  for id in $(body | j "d.requests.map(r => r.id).join(' ')"); do call DELETE "/requests/$id" "$1" > /dev/null; done
  call GET /trips/upcoming "$1" > /dev/null
  for id in $(body | j "d.trips.map(t => t.role + ':' + t.id).join(' ')"); do
    kind=${id%%:*}; id=${id#*:}
    if [ "$kind" = driver ]; then
      call DELETE "/trips/$id" "$1" > /dev/null
    else
      call GET "/trips/$id" "$1" > /dev/null
      call DELETE "/bookings/$(body | j 'd.booking_id')" "$1" > /dev/null
    fi
  done
  call GET /asks/pending "$1" > /dev/null
  for id in $(body | j "d.asks.map(a => a.id).join(' ')"); do call POST "/asks/$id/decline" "$1" > /dev/null; done
  call GET /me/routes "$1" > /dev/null
  for id in $(body | j "d.routes.map(r => r.id).join(' ')"); do call DELETE "/me/routes/$id" "$1" > /dev/null; done
  # What cannot be unpicked through the API: a pending ask on a request that
  # is gone is harmless, and a departure called off stays so.
  return 0
}

# Drivers first: calling off a departure sends its passengers' requests back to
# the queue, a moment later, from the dispatcher — so the passengers are cleared
# once that has happened, or the request would be reopened behind them.
reset_all() {
  local name t=0
  for name in D D2 D3; do reset_account "$name"; done
  while [ "$(q "SELECT count(*) FROM outbox WHERE kind = 'reopen_request' AND done_at IS NULL AND dead_at IS NULL")" != 0 ] && [ "$t" -lt 40 ]; do
    sleep 2; t=$((t + 2))
  done
  for name in P1 P2 P3 P4 P5 P6 X; do reset_account "$name"; done
}

# ---- time, places, routes ------------------------------------------------------

day() { TZ=Europe/Athens date -d "+$1 day" +%F; }
# utc DAY HH:MM -> that Athens wall time as an instant
utc() { date -u -d "TZ=\"Europe/Athens\" $1 $2" +%FT%TZ; }
hm() { TZ=Europe/Athens date -d "$1" +%H:%M; }
today_athens() { TZ=Europe/Athens date +%F; }

# Places on the road, from /v1/places/near on the E90 and the villages around it.
MESTI="25.647265 40.952773"
AVRA="25.68308 40.925634"
DIONI="25.562828 40.979716"
SALMONI="25.535186 41.005243"
AMARANTA="25.499159 41.050408"
DIKELLA="25.696804 40.864852"
# A point a fraction of the way along a departure's road, as "lon lat".
along() { q "SELECT ST_X(p) || ' ' || ST_Y(p) FROM (SELECT ST_LineInterpolatePoint(route_geom, $2) p FROM trip_occurrences WHERE id = '$1') x"; }
# Where along the road (0..1) a point falls.
fraction() { local p=($2); q "SELECT round(ST_LineLocatePoint(route_geom, ST_SetSRID(ST_MakePoint(${p[0]}, ${p[1]}), 4326))::numeric, 3) FROM trip_occurrences WHERE id = '$1'"; }

# publish NAME DAY FROM TO SEATS DETOUR [extra json fields, leading comma]
# -> R_TPL, R_OCC (the first departure)
publish() {
  local resp
  resp=$(curl -s -X POST "$API/routes" -H "Authorization: Bearer $(tok "$1")" -H 'Content-Type: application/json' -d "{
    \"origin_lon\":25.8743,\"origin_lat\":40.8476,\"destination_lon\":25.4048,\"destination_lat\":41.1224,
    \"origin_label\":\"Αλεξανδρούπολη\",\"destination_label\":\"Κομοτηνή\",\"seats\":$5,\"detour_min\":$6,
    \"kind\":\"oneoff\",\"oneoff_date\":\"$2\",\"depart_earliest\":\"$3\",\"depart_latest\":\"$4\"${7:-}}")
  R_TPL=$(printf '%s' "$resp" | j "d.template_id || ''")
  R_OCC=$(printf '%s' "$resp" | j "d.first_occurrence_id || ''")
  [ -n "$R_TPL" ] && [ -n "$R_OCC" ] || { echo "publish failed: $resp" >&2; return 1; }
}

# ride NAME DAY FROM(lon lat) TO(lon lat) SEATS [LUGGAGE COUNT [ALTERNATIVE_OF]]
# -> R_HTTP, R_REQ, R_ASK (empty when nobody was asked)
ride() {
  local o=($3) d=($4) extra=""
  [ -n "${6:-}" ] && extra="$extra,\"luggage\":\"$6\",\"luggage_count\":$7"
  [ -n "${8:-}" ] && extra="$extra,\"alternative_of\":\"$8\""
  R_HTTP=$(call POST /requests "$1" "{
    \"origin_lon\":${o[0]},\"origin_lat\":${o[1]},\"destination_lon\":${d[0]},\"destination_lat\":${d[1]},
    \"origin_label\":\"Στον δρόμο\",\"destination_label\":\"Στον δρόμο\",\"seats\":$5,
    \"window_start\":\"$(utc "$2" 07:00)\",\"window_end\":\"$(utc "$2" 10:00)\"$extra}")
  R_REQ=$(body | j "d.request_id || ''")
  R_ASK=$(body | j "d.ask_id || ''")
}

# until_true SECONDS command... — polls a command (exit 0 = done).
until_true() {
  local limit=$1 t=0; shift
  while ! "$@" 2>/dev/null; do
    t=$((t + 2)); [ "$t" -le "$limit" ] || return 1; sleep 2
  done
}
sql_is() { [ "$(q "$1")" = "$2" ]; }
sql_isnt() { [ "$(q "$1")" != "$2" ]; }
pending_ask_for() { # request id -> ask id on stdout when there is one
  local a; a=$(q "SELECT id FROM asks WHERE request_id = '$1' AND status = 'pending'")
  [ -n "$a" ] && printf '%s' "$a"
}
seats_free() { q "SELECT seats_free FROM trip_occurrences WHERE id = '$1'"; }
accept() { # driver ask -> sets R_BOOKING; HTTP in R_HTTP
  R_HTTP=$(call POST "/asks/$2/accept" "$1")
  R_BOOKING=$(body | j "d.booking_id || ''")
}
SOON_WAKE=90 # seconds a woken request may take to be asked

# ---- 1 ---------------------------------------------------------------------------

s1_first_passenger() {
  local d1 mine
  d1=$(day 1)
  publish D "$d1" 08:00 08:30 3 10
  state_set TPL1 "$R_TPL"; state_set OCC1 "$R_OCC"
  ok "D published $d1 08:00 with 3 seats and a 10 minute detour: template $R_TPL, departure $R_OCC"
  must_eq "seats_free before anyone" "$(seats_free "$R_OCC")" 3

  ride P1 "$d1" "$(along "$R_OCC" 0.05)" "$(along "$R_OCC" 0.6)" 1
  state_set REQ1 "$R_REQ"
  must_eq "P1's request answered" "$R_HTTP" 201
  [ -n "$R_ASK" ] || fail "P1 was not asked (request $R_REQ: $(body); mine: $(call GET /requests/mine P1 >/dev/null; body))"
  ok "P1 was asked at once: ask $R_ASK"
  state_set ASK1 "$R_ASK"

  call GET /asks/pending D > /dev/null
  mine=$(body | j "JSON.stringify(d.asks.find(a => a.id === '$R_ASK') || null)")
  [ "$mine" != null ] || fail "D's pending asks do not list the ask: $(body)"
  ok "D sees it: $mine"
  # Nobody else can answer for D.
  eq "a stranger accepts D's offer" "$(call POST "/asks/$R_ASK/accept" X)" 403
  eq "another driver accepts it" "$(call POST "/asks/$R_ASK/accept" D2)" 403
  eq "the passenger accepts their own offer" "$(call POST "/asks/$R_ASK/accept" P1)" 403
  eq "a stranger declines it" "$(call POST "/asks/$R_ASK/decline" X)" 403
  eq "a stranger opens the ask" "$(call GET "/asks/$R_ASK" X)" 404
  eq "the offer is still pending" "$(q "SELECT status FROM asks WHERE id = '$R_ASK'")" pending

  accept D "$R_ASK"
  must_eq "D accepts" "$R_HTTP" 200
  [ -n "$R_BOOKING" ] || fail "no booking id: $(body)"
  state_set BOOKING1 "$R_BOOKING"
  eq "seats_free after P1's seat" "$(seats_free "$(state_get OCC1)")" 2
  eq "request status" "$(q "SELECT status FROM ride_requests WHERE id = '$(state_get REQ1)'")" matched
  eq "booking row" "$(q "SELECT status || ' seats=' || seats FROM bookings WHERE id = '$R_BOOKING'")" "confirmed seats=1"
  eq "push queued for the driver's offer" "$(q "SELECT count(*) FROM outbox WHERE idempotency_key = 'ask:$(state_get ASK1)'")" 1
  until_true 60 sql_is "SELECT count(*) FROM outbox WHERE idempotency_key = 'booking_confirmed:$R_BOOKING' AND done_at IS NOT NULL" 1 \
    && ok "booking_confirmed push delivered to P1" || soft "booking_confirmed push not done in 60s: $(q "SELECT coalesce(last_error,'-') || ' attempts=' || attempts || ' dead=' || coalesce(dead_at::text,'-') FROM outbox WHERE idempotency_key = 'booking_confirmed:$R_BOOKING'")"
}

# ---- 2 ---------------------------------------------------------------------------

s2_party_of_two() {
  local occ d1 trip i
  occ=$(state_get OCC1); [ -n "$occ" ] || fail "scenario 1 left no departure"
  d1=$(day 1)
  ride P2 "$d1" "$MESTI" "$(along "$occ" 0.8)" 2
  state_set REQ2 "$R_REQ"
  must_eq "P2's request answered" "$R_HTTP" 201
  [ -n "$R_ASK" ] || fail "P2 (party of 2 from Μέστη) was not asked: $(body); $(call GET /requests/mine P2 >/dev/null; body)"
  ok "P2 was asked: ask $R_ASK (detour $(q "SELECT detour_min || ' min, ' || detour_km || ' km' FROM asks WHERE id = '$R_ASK'"))"
  accept D "$R_ASK"
  must_eq "D accepts" "$R_HTTP" 200
  state_set BOOKING2 "$R_BOOKING"
  must_eq "seats_free" "$(seats_free "$occ")" 0

  # The itinerary: four stops, pickups before drop-offs, in road order.
  call GET "/trips/$occ" D > /dev/null
  trip=$(body)
  eq "D's trip lists passengers" "$(printf '%s' "$trip" | j 'd.passengers.length')" 2
  eq "D's trip says seats_free" "$(printf '%s' "$trip" | j 'd.seats_free')" 0
  local stops
  stops=$(q "SELECT string_agg(kind || ':' || who || '@' || f, ' ' ORDER BY f, kind DESC) FROM (
      SELECT 'pickup' AS kind, p.name AS who, round(ST_LineLocatePoint(o.route_geom, b.pickup_point)::numeric, 3) AS f
        FROM bookings b JOIN users p ON p.id = b.passenger_id JOIN trip_occurrences o ON o.id = b.occurrence_id
       WHERE b.occurrence_id = '$occ' AND b.status = 'confirmed'
      UNION ALL
      SELECT 'dropoff', p.name, round(ST_LineLocatePoint(o.route_geom, b.dropoff_point)::numeric, 3)
        FROM bookings b JOIN users p ON p.id = b.passenger_id JOIN trip_occurrences o ON o.id = b.occurrence_id
       WHERE b.occurrence_id = '$occ' AND b.status = 'confirmed') s")
  local n
  n=$(printf '%s' "$trip" | j 'd.passengers.filter(p => p.pickup && p.dropoff).length * 2')
  eq "stops D can hand to the maps app (pickup + dropoff each)" "$n" 4
  # Valid order: every pickup ahead of its own drop-off.
  local bad
  bad=$(q "SELECT count(*) FROM bookings b JOIN trip_occurrences o ON o.id = b.occurrence_id
            WHERE b.occurrence_id = '$occ' AND b.status = 'confirmed'
              AND ST_LineLocatePoint(o.route_geom, b.pickup_point) >= ST_LineLocatePoint(o.route_geom, b.dropoff_point)")
  eq "bookings with a pickup not ahead of the drop-off" "$bad" 0
  ok "stops along the road: $stops"

  # Who sees whom.
  call GET "/trips/$occ" P1 > /dev/null
  # P1 cancelled? no — still booked here: P1 is in the car
  check "P1 sees the driver" "$(body | j "d.driver && d.driver.name ? 0 : 1")" "driver $(body | j 'd.driver.name') ($(body | j 'd.driver.car_model + " " + d.driver.car_plate'))"
  check "P1 sees P2 in the car" "$(body | j "d.passengers.some(p => p.name === 'QT P2') ? 0 : 1")" "passengers: $(body | j "d.passengers.map(p => p.name).join(', ')")"
  check "P1 is not given P2's pickup or drop-off" \
    "$(body | j "(d.passengers.find(p => p.name === 'QT P2') || {}).pickup == null && (d.passengers.find(p => p.name === 'QT P2') || {}).dropoff == null ? 0 : 1")" "P2 entry: $(body | j "JSON.stringify((d.passengers.find(p => p.name === 'QT P2') || {}))")"
  check "P1 is not shown P2's price" \
    "$(body | j "(d.passengers.find(p => p.name === 'QT P2') || {}).price_eur == null ? 0 : 1")" "P2's price_eur as P1 sees it: $(body | j "(d.passengers.find(p => p.name === 'QT P2') || {}).price_eur")"
  call GET "/trips/$occ" P2 > /dev/null
  check "P2 sees the driver" "$(body | j "d.driver && d.driver.name ? 0 : 1")" "driver $(body | j 'd.driver.name')"
  check "P2 sees P1 in the car" "$(body | j "d.passengers.some(p => p.name === 'QT P1') ? 0 : 1")" "passengers: $(body | j "d.passengers.map(p => p.name).join(', ')")"
  check "P2 is not shown the driver's destination" "$(body | j "d.destination == null ? 0 : 1")" "destination=$(body | j 'JSON.stringify(d.destination)')"
}

# ---- 3 ---------------------------------------------------------------------------

s3_full_car() {
  local occ d1
  occ=$(state_get OCC1); [ -n "$occ" ] || fail "scenario 1 left no departure"
  d1=$(day 1)
  ride P3 "$d1" "$(along "$occ" 0.7)" "$(along "$occ" 0.92)" 1
  state_set REQ3 "$R_REQ"
  must_eq "P3's request accepted" "$R_HTTP" 201
  eq "nobody asked (ask_id)" "${R_ASK:-none}" none
  eq "asks rows for the request" "$(q "SELECT count(*) FROM asks WHERE request_id = '$R_REQ'")" 0
  eq "request status" "$(q "SELECT status FROM ride_requests WHERE id = '$R_REQ'")" open
  call GET /requests/mine P3 > /dev/null
  local mine
  mine=$(body | j "JSON.stringify(d.requests.find(r => r.id === '$R_REQ'))")
  ok "/requests/mine says: $mine"
  check "asking_now is false" "$(printf '%s' "$mine" | j "d.asking_now === false ? 0 : 1")" "asking_now=$(printf '%s' "$mine" | j 'd.asking_now')"
  check "one driver is known on the corridor" "$(printf '%s' "$mine" | j "d.drivers_on_corridor >= 1 ? 0 : 1")" "drivers_on_corridor=$(printf '%s' "$mine" | j 'd.drivers_on_corridor')"
  # Why: the car is full. Is there anything in the answer that says so — a
  # departure with no seat, a missed reason, a reason of any kind?
  check "the answer says why nobody is asked (full car)" \
    "$(printf '%s' "$mine" | j "Object.keys(d).some(k => /full|reason|seats_left|capacity/.test(k) && d[k] != null && k !== 'most_seats_on_corridor') || (d.missed && d.missed.reason) ? 0 : 1")" \
    "fields: drivers_asked=$(printf '%s' "$mine" | j 'd.drivers_asked') untried=$(printf '%s' "$mine" | j 'd.drivers_untried') departing=$(printf '%s' "$mine" | j 'd.drivers_departing') missed=$(printf '%s' "$mine" | j 'JSON.stringify(d.missed)') most_seats=$(printf '%s' "$mine" | j 'd.most_seats_on_corridor')"
}

# ---- 4 ---------------------------------------------------------------------------

s4_cancel_wakes_waiter() {
  local occ b1 req3 ask
  occ=$(state_get OCC1); b1=$(state_get BOOKING1); req3=$(state_get REQ3)
  [ -n "$occ" ] && [ -n "$b1" ] && [ -n "$req3" ] || fail "scenarios 1-3 left nothing to cancel"
  must_eq "P1 cancels the booking" "$(call DELETE "/bookings/$b1" P1)" 204
  eq "seats_free back to" "$(seats_free "$occ")" 1
  eq "P1 cancels again" "$(call DELETE "/bookings/$b1" P1)" 204
  eq "seats_free after the second cancel (not given back twice)" "$(seats_free "$occ")" 1
  eq "P1's booking" "$(q "SELECT status FROM bookings WHERE id = '$b1'")" cancelled_by_passenger
  eq "D is told (outbox)" "$(q "SELECT count(*) FROM outbox WHERE payload->>'booking_id' = '$b1' AND payload->>'event' = 'booking_cancelled_for_driver'")" 1
  eq "P1's request after cancelling the booking" "$(q "SELECT status FROM ride_requests WHERE id = '$(state_get REQ1)'")" matched
  echo "   waiting for P3 to be woken (up to ${SOON_WAKE}s)"
  until_true "$SOON_WAKE" pending_ask_for "$req3" > /dev/null || fail "P3 was not asked within ${SOON_WAKE}s of the seat coming back: $(call GET /requests/mine P3 >/dev/null; body)"
  ask=$(pending_ask_for "$req3")
  ok "P3 woken and asked: ask $ask (seconds after the cancel: $(q "SELECT round(extract(epoch FROM a.created_at - b.updated_at)) FROM asks a, bookings b WHERE a.id = '$ask' AND b.id = '$b1'"))"
  call GET /asks/pending D > /dev/null
  check "D sees it in /asks/pending" "$(body | j "d.asks.some(a => a.id === '$ask') ? 0 : 1")" "$(body | j 'd.asks.length') pending"
  accept D "$ask"
  must_eq "D accepts P3" "$R_HTTP" 200
  state_set BOOKING3 "$R_BOOKING"
  eq "seats_free" "$(seats_free "$occ")" 0
  # 403 or 404 both refuse; trips answer 404 so ids cannot be probed, bookings 403.
  for who in P2 X; do
    case "$(call DELETE "/bookings/$R_BOOKING" "$who")" in
      403|404) ok "$who cancels P3's booking: refused ($(err_code))";;
      *) soft "$who cancelled, or got an odd answer for, P3's booking";;
    esac
  done
  eq "P3's booking after those" "$(q "SELECT status FROM bookings WHERE id = '$R_BOOKING'")" confirmed
  eq "bookings aboard" "$(q "SELECT string_agg(u.name || ' x' || b.seats, ', ' ORDER BY u.name) FROM bookings b JOIN users u ON u.id = b.passenger_id WHERE b.occurrence_id = '$occ' AND b.status = 'confirmed'")" "QT P2 x2, QT P3 x1"
}

# ---- 10 --------------------------------------------------------------------------

s10_chat() {
  local occ n who
  occ=$(state_get OCC1); [ -n "$occ" ] || fail "scenario 1 left no departure"
  [ -n "$(state_get BOOKING3)" ] || fail "scenario 4 did not seat P3, so there is no car with D, P2 and P3 in it"
  # P1 had a seat and gave it back in scenario 4.
  for who in D P2 P3; do
    must_eq "$who posts" "$(call POST "/trips/$occ/messages" "$who" "{\"body\":\"hello from $who\"}")" 201
  done
  for who in D P2 P3; do
    call GET "/trips/$occ/messages" "$who" > /dev/null
    n=$(body | j 'd.messages.length')
    eq "$who reads all three" "$n" 3
  done
  eq "the thread, in order" "$(body | j "d.messages.map(m => m.sender_name + ':' + m.body).join(' | ')")" "QT D:hello from D | QT P2:hello from P2 | QT P3:hello from P3" 
  # The seat P1 gave back is no seat at all.
  eq "P1 (booking cancelled) reads" "$(call GET "/trips/$occ/messages" P1)" 404
  eq "P1 (booking cancelled) posts" "$(call POST "/trips/$occ/messages" P1 '{"body":"let me back in"}')" 404
  eq "a stranger reads" "$(call GET "/trips/$occ/messages" X)" 404
  eq "a stranger posts" "$(call POST "/trips/$occ/messages" X '{"body":"hi"}')" 404
  eq "a stranger asks for the trip card" "$(call GET "/trips/$occ" X)" 404
  eq "nothing from P1 or the stranger got in" "$(q "SELECT count(*) FROM trip_messages WHERE occurrence_id = '$occ' AND body IN ('let me back in', 'hi')")" 0
  eq "an empty message" "$(call POST "/trips/$occ/messages" D '{"body":"   "}')" 400
  eq "a message of 2001 characters" "$(call POST "/trips/$occ/messages" D "{\"body\":\"$(head -c 2001 /dev/zero | tr '\0' 'x')\"}")" 400
  eq "reading with half a cursor" "$(call GET "/trips/$occ/messages?after_id=$(uid_of D)" D)" 400
}

# ---- 11 --------------------------------------------------------------------------

# Window: from 30 minutes before the earliest departure to an hour after the
# trip could have arrived (internal/live). Before it, a position is refused;
# in it, the driver sees the passengers who share and each passenger sees the
# driver and nobody else.
s11_live() {
  local occ occ3 opens now_t d p1 p2 pos
  occ=$(state_get OCC1); [ -n "$occ" ] || fail "scenario 1 left no departure"

  # Before the window: tomorrow's 08:00 opens at 07:30 tomorrow.
  pos=$(along "$occ" 0.02); local pp=($pos)
  eq "D shares before the window" "$(call PUT "/trips/$occ/position" D "{\"lat\":${pp[1]},\"lon\":${pp[0]}}")" 409
  eq "the refusal's code" "$(err_code)" not_sharing_now
  eq "P2 shares before the window" "$(call PUT "/trips/$occ/position" P2 "{\"lat\":${pp[1]},\"lon\":${pp[0]}}")" 409
  eq "P2 reads before the window" "$(call GET "/trips/$occ/live" P2)" 200
  opens=$(body | j 'd.opens')
  eq "positions before the window" "$(body | j 'd.positions.length')" 0
  eq "the window opens at" "$(TZ=Europe/Athens date -d "$opens" +%F' '%H:%M)" "$(day 1) 07:30"
  eq "P1 (booking cancelled) reads the live map" "$(call GET "/trips/$occ/live" P1)" 404
  eq "a stranger reads the live map" "$(call GET "/trips/$occ/live" X)" 404
  eq "a stranger shares" "$(call PUT "/trips/$occ/position" X "{\"lat\":${pp[1]},\"lon\":${pp[0]}}")" 404

  # In the window: a departure in twelve minutes, today.
  now_t=$(TZ=Europe/Athens date +%H%M)
  if [ "$now_t" -ge 2250 ]; then
    soft "too close to midnight to publish a departure for the next hour; run again earlier"; return
  fi
  d=$(today_athens)
  publish D3 "$d" "$(hm '+12 minutes')" "$(hm '+40 minutes')" 3 15
  occ3=$R_OCC
  ok "D3 leaves at $(hm '+12 minutes') today: departure $occ3, live window opened at $(q "SELECT to_char(depart_earliest - interval '30 minutes', 'HH24:MI') FROM trip_occurrences WHERE id = '$occ3'" )"
  local from_t to_t
  from_t=$(date -u -d '+1 minute' +%FT%TZ); to_t=$(date -u -d '+3 hours' +%FT%TZ)
  local seat
  for who in P1 P2; do
    if [ "$who" = P1 ]; then o=($(along "$occ3" 0.05)); e=($(along "$occ3" 0.6)); else o=($MESTI); e=($(along "$occ3" 0.8)); fi
    R_HTTP=$(call POST /requests "$who" "{\"origin_lon\":${o[0]},\"origin_lat\":${o[1]},\"destination_lon\":${e[0]},\"destination_lat\":${e[1]},
      \"origin_label\":\"Στον δρόμο\",\"destination_label\":\"Στον δρόμο\",\"seats\":1,\"window_start\":\"$from_t\",\"window_end\":\"$to_t\"}")
    R_ASK=$(body | j "d.ask_id || ''")
    [ -n "$R_ASK" ] || fail "$who was not asked for the departure in twelve minutes: $(body)"
    accept D3 "$R_ASK"
    must_eq "$who gets a seat" "$R_HTTP" 200
  done

  p1=($(along "$occ3" 0.05)); p2=($MESTI); local dpos=($(along "$occ3" 0.03))
  must_eq "D3 shares" "$(call PUT "/trips/$occ3/position" D3 "{\"lat\":${dpos[1]},\"lon\":${dpos[0]},\"accuracy_m\":10,\"observed_at\":\"$(date -u +%FT%TZ)\"}")" 204
  must_eq "P1 shares" "$(call PUT "/trips/$occ3/position" P1 "{\"lat\":${p1[1]},\"lon\":${p1[0]}}")" 204
  must_eq "P2 shares" "$(call PUT "/trips/$occ3/position" P2 "{\"lat\":${p2[1]},\"lon\":${p2[0]}}")" 204
  call GET "/trips/$occ3/live" D3 > /dev/null
  eq "D3 sees" "$(body | j "d.positions.map(p => p.name).sort().join(',')")" "QT P1,QT P2"
  call GET "/trips/$occ3/live" P1 > /dev/null
  eq "P1 sees" "$(body | j "d.positions.map(p => p.name).join(',')")" "QT D3"
  eq "P1's view carries no P2 coordinates" "$(body | j "JSON.stringify(d).includes('${p2[0]}') ? 'leak' : 'none'")" none
  call GET "/trips/$occ3/live" P2 > /dev/null
  eq "P2 sees" "$(body | j "d.positions.map(p => p.name).join(',')")" "QT D3"
  eq "a stranger reads the live map" "$(call GET "/trips/$occ3/live" X)" 404
  eq "a stranger shares" "$(call PUT "/trips/$occ3/position" X "{\"lat\":${p2[1]},\"lon\":${p2[0]}}")" 404
  # P1 stops sharing: D3 no longer sees them.
  must_eq "P1 stops sharing" "$(call DELETE "/trips/$occ3/position" P1)" 204
  call GET "/trips/$occ3/live" D3 > /dev/null
  eq "D3 sees after P1 stops" "$(body | j "d.positions.map(p => p.name).join(',')")" "QT P2"
  eq "a position off the planet" "$(call PUT "/trips/$occ3/position" P2 '{"lat":123,"lon":25}')" 400
  eq "a position missing its longitude" "$(call PUT "/trips/$occ3/position" P2 '{"lat":40}')" 400
  eq "a fix taken ten minutes ago" "$(call PUT "/trips/$occ3/position" P2 "{\"lat\":${p2[1]},\"lon\":${p2[0]},\"observed_at\":\"$(date -u -d '-10 minutes' +%FT%TZ)\"}")" 400
}

# ---- 5 ---------------------------------------------------------------------------

# Two parties aboard and a seat to spare: a third party is not asked
# (match.MaxConcurrentParties). What the passenger is told, and whether the cap
# holds when the asks were all made before any was answered.
s5_party_cap() {
  local d occ ask mine codes
  d=$(day 2)
  publish D2 "$d" 08:00 08:30 4 20
  occ=$R_OCC
  ok "D2 offers 4 seats on $d: departure $occ"
  ride P1 "$d" "$(along "$occ" 0.05)" "$(along "$occ" 0.5)" 1
  [ -n "$R_ASK" ] || fail "P1 not asked: $(body)"; accept D2 "$R_ASK"; must_eq "P1 aboard" "$R_HTTP" 200
  ride P2 "$d" "$(along "$occ" 0.3)" "$(along "$occ" 0.8)" 1
  [ -n "$R_ASK" ] || fail "P2 not asked: $(body)"; accept D2 "$R_ASK"; must_eq "P2 aboard" "$R_HTTP" 200
  eq "seats_free with two parties of one aboard" "$(seats_free "$occ")" 2
  ride P3 "$d" "$(along "$occ" 0.6)" "$(along "$occ" 0.9)" 1
  state_set REQ5 "$R_REQ"
  must_eq "P3's request answered" "$R_HTTP" 201
  eq "P3 asked although two parties are aboard" "${R_ASK:-none}" none
  eq "asks rows for P3" "$(q "SELECT count(*) FROM asks WHERE request_id = '$R_REQ'")" 0
  call GET /requests/mine P3 > /dev/null
  mine=$(body | j "JSON.stringify(d.requests.find(r => r.id === '$R_REQ'))")
  ok "/requests/mine says: $mine"
  check "P3 is told why (party cap)" \
    "$(printf '%s' "$mine" | j "d.missed ? 0 : 1")" \
    "missed=$(printf '%s' "$mine" | j 'JSON.stringify(d.missed)') — a seat is free (seats_free 2) yet the answer reads as 'nobody asked yet'"
  # The cap lifts when a party leaves.
  must_eq "P2 cancels" "$(call DELETE "/bookings/$(q "SELECT id FROM bookings WHERE occurrence_id = '$occ' AND passenger_id = '$(uid_of P2)' AND status = 'confirmed'")" P2)" 204
  until_true "$SOON_WAKE" pending_ask_for "$(state_get REQ5)" > /dev/null || fail "P3 not asked within ${SOON_WAKE}s after a party left"
  ok "P3 asked once a party left: $(pending_ask_for "$(state_get REQ5)")"
  call DELETE "/requests/$(state_get REQ5)" P3 > /dev/null

  # All three asked before anybody answers: the cap is enforced at ask time
  # against bookings, so all three offers are out together.
  d=$(day 3)
  publish D2 "$d" 08:00 08:30 4 20
  occ=$R_OCC
  local asks=() who i=0 from to
  for who in P1 P2 P3; do
    from=(0.05 0.2 0.35); to=(0.5 0.7 0.9)
    ride "$who" "$d" "$(along "$occ" "${from[$i]}")" "$(along "$occ" "${to[$i]}")" 1
    [ -n "$R_ASK" ] || fail "$who not asked on $d: $(body)"
    asks+=("$R_ASK"); i=$((i + 1))
  done
  ok "three offers out at once: ${#asks[@]} pending asks on one 4-seat car"
  codes=""
  for i in 0 1 2; do
    R_HTTP=$(call POST "/asks/${asks[$i]}/accept" D2)
    codes="$codes $R_HTTP($(err_code))"
  done
  ok "D2 accepts them one after another: $codes"
  local aboard
  aboard=$(q "SELECT count(*) FROM bookings WHERE occurrence_id = '$occ' AND status = 'confirmed'")
  check "the first two of three simultaneous offers can both be taken (4 seats, cap of 2 parties)" \
    "$([ "$aboard" = 2 ] && echo 0 || echo 1)" "parties aboard after three accepts: $aboard (codes:$codes)"
  check "no more than two parties aboard (MaxConcurrentParties=2)" "$([ "$aboard" -le 2 ] && echo 0 || echo 1)" "bookings confirmed: $aboard; seats_free $(seats_free "$occ")"
  check "no accept answered 5xx" "$(case "$codes" in *" 5"*) echo 1;; *) echo 0;; esac)" "codes:$codes"
}

# ---- 6 ---------------------------------------------------------------------------

s6_races() {
  local d occ a b c1 c2 asks n
  # 6a: the last seat requested together.
  d=$(day 4)
  publish D "$d" 08:00 08:30 1 15
  occ=$R_OCC
  local pf=$(along "$occ" 0.1) pt=$(along "$occ" 0.7)
  local of=($pf) ot=($pt)
  for who in P5 P6; do
    ( OUT=$TMP/race-$who curl -s -o "$TMP/race-$who" -w '%{http_code}' -X POST "$API/requests" \
        -H "Authorization: Bearer $(tok "$who")" -H 'Content-Type: application/json' -d "{
        \"origin_lon\":${of[0]},\"origin_lat\":${of[1]},\"destination_lon\":${ot[0]},\"destination_lat\":${ot[1]},
        \"origin_label\":\"Στον δρόμο\",\"destination_label\":\"Στον δρόμο\",\"seats\":1,
        \"window_start\":\"$(utc "$d" 07:00)\",\"window_end\":\"$(utc "$d" 10:00)\"}" > "$TMP/race-$who.code" ) &
  done
  wait
  ok "both requests answered: P5 $(cat "$TMP/race-P5.code"), P6 $(cat "$TMP/race-P6.code")"
  a=$(j "d.ask_id || ''" < "$TMP/race-P5"); b=$(j "d.ask_id || ''" < "$TMP/race-P6")
  eq "pending asks on the one-seat car" "$(q "SELECT count(*) FROM asks WHERE occurrence_id = '$occ' AND status = 'pending'")" 1
  eq "requests holding an ask (ids in the replies)" "$(printf '%s\n%s\n' "$a" "$b" | grep -c .)" 1
  eq "the other request is" "$(q "SELECT string_agg(status, ',' ORDER BY status) FROM ride_requests WHERE passenger_id IN ('$(uid_of P5)', '$(uid_of P6)') AND window_start >= '$(utc "$d" 00:00)' AND status IN ('open','asking')")" "asking,open"
  ask=${a:-$b}
  # The same post again is the same request.
  local first_req
  first_req=$(q "SELECT id FROM ride_requests WHERE id IN ('$(j "d.request_id" < "$TMP/race-P5")', '$(j "d.request_id" < "$TMP/race-P6")') AND status = 'asking'")
  ok "the request holding the ask: $first_req"
  # 6b: the driver's two taps on the same offer.
  ( OUT=$TMP/acc1 call POST "/asks/$ask/accept" D > "$TMP/acc1.code" ) &
  ( OUT=$TMP/acc2 call POST "/asks/$ask/accept" D > "$TMP/acc2.code" ) &
  wait
  codes=$(printf '%s\n%s\n' "$(cat "$TMP/acc1.code")" "$(cat "$TMP/acc2.code")" | sort | tr '\n' ' ')
  eq "two accepts of one ask" "$codes" "200 409 "
  eq "the loser's code" "$(j "d.error && d.error.code || d.code || ''" < "$TMP/acc1" 2>/dev/null)$(j "d.error && d.error.code || d.code || ''" < "$TMP/acc2" 2>/dev/null)" "ask_closed"
  eq "bookings on the car" "$(q "SELECT count(*) FROM bookings WHERE occurrence_id = '$occ' AND status = 'confirmed'")" 1
  eq "seats_free" "$(seats_free "$occ")" 0
  local winner; winner=$(q "SELECT p.name FROM bookings b JOIN users p ON p.id = b.passenger_id WHERE b.occurrence_id = '$occ'")
  case $winner in "QT P5") winner=P5;; *) winner=P6;; esac
  eq "the winner cancels the request that is already a booking" "$(call DELETE "/requests/$first_req" "$winner")" 409
  eq "  code" "$(err_code)" already_matched
  # The other passenger is not asked about a full car.
  sleep 4
  eq "pending asks after the seat went" "$(q "SELECT count(*) FROM asks WHERE occurrence_id = '$occ' AND status = 'pending'")" 0

  # 6c: two offers out together on a two-seat car, accepted together.
  d=$(day 5)
  publish D "$d" 08:00 08:30 2 15
  occ=$R_OCC
  ride P1 "$d" "$(along "$occ" 0.05)" "$(along "$occ" 0.5)" 1; c1=$R_ASK
  ride P2 "$d" "$(along "$occ" 0.3)" "$(along "$occ" 0.8)" 1; c2=$R_ASK
  [ -n "$c1" ] && [ -n "$c2" ] || fail "two offers on a two-seat car: P1 '$c1', P2 '$c2'"
  ok "two offers out on two seats: $c1, $c2"
  ( OUT=$TMP/p1 call POST "/asks/$c1/accept" D > "$TMP/p1.code" ) &
  ( OUT=$TMP/p2 call POST "/asks/$c2/accept" D > "$TMP/p2.code" ) &
  wait
  ok "parallel accepts: $(cat "$TMP/p1.code")($(j "d.error && d.error.code || 'booked'" < "$TMP/p1")) and $(cat "$TMP/p2.code")($(j "d.error && d.error.code || 'booked'" < "$TMP/p2"))"
  n=$(q "SELECT count(*) FROM bookings WHERE occurrence_id = '$occ' AND status = 'confirmed'")
  eq "seats_free = 2 - bookings" "$(seats_free "$occ")" "$((2 - n))"
  case "$(cat "$TMP/p1.code")$(cat "$TMP/p2.code")" in *5??*) check "no 5xx" 1 "codes $(cat "$TMP/p1.code") $(cat "$TMP/p2.code")";; *) ok "no 5xx: codes $(cat "$TMP/p1.code") $(cat "$TMP/p2.code")";; esac
  check "both offers could be taken: two seats, two single passengers" "$([ "$n" = 2 ] && echo 0 || echo 1)" "bookings $n"
  if [ "$n" != 2 ]; then
    local lost loser lreq
    if [ "$(cat "$TMP/p1.code")" != 200 ]; then lost=$c1; loser=P1; else lost=$c2; loser=P2; fi
    lreq=$(q "SELECT request_id FROM asks WHERE id = '$lost'")
    ok "the loser's ask $lost is now $(q "SELECT status FROM asks WHERE id = '$lost'"); accepting it again: $(call POST "/asks/$lost/accept" D) $(err_code)"
    # And meanwhile: a second car on the same morning. The loser's request still
    # holds a live offer that cannot be accepted, so nobody else is asked.
    publish D2 "$d" 08:00 08:30 3 15
    sleep 15
    check "the loser ($loser) is offered the second car while their first offer is dead" \
      "$([ "$(q "SELECT count(*) FROM asks WHERE request_id = '$lreq'")" -ge 2 ] && echo 0 || echo 1)" \
      "asks on the request: $(q "SELECT string_agg(status, ',' ORDER BY created_at) FROM asks WHERE request_id = '$lreq'") — the unique pending ask per request keeps the dead one in the way until it expires"
    call GET /requests/mine "$loser" > /dev/null
    ok "$loser sees: asking_now=$(body | j 'd.requests[0].asking_now') until $(body | j 'd.requests[0].ask_expires_at')"
  fi
}

# ---- 7 ---------------------------------------------------------------------------

s7_decline_expiry() {
  local d req ask who first second third left name
  d=$(day 7)
  declare -A OCCOF
  for who in D D2 D3; do publish "$who" "$d" 08:00 08:30 3 15; OCCOF[$who]=$R_OCC; done
  ride P1 "$d" "$(along "${OCCOF[D]}" 0.05)" "$(along "${OCCOF[D]}" 0.6)" 1
  req=$R_REQ
  [ -n "$R_ASK" ] || fail "P1 not asked with three cars out: $(body)"
  owner_of() { q "SELECT u.name FROM asks a JOIN trip_occurrences o ON o.id = a.occurrence_id JOIN route_templates t ON t.id = o.template_id JOIN users u ON u.id = t.driver_id WHERE a.id = '$1'"; }
  name_of() { case "$1" in "QT D") echo D;; "QT D2") echo D2;; "QT D3") echo D3;; esac; }
  first=$(name_of "$(owner_of "$R_ASK")"); ask=$R_ASK
  ok "first offer went to $first (ask $ask, expires in $(q "SELECT round(extract(epoch FROM expires_at - now())/60) FROM asks WHERE id = '$ask'") min)"
  eq "outbox push for the offer" "$(q "SELECT count(*) FROM outbox WHERE idempotency_key = 'ask:$ask'")" 1
  must_eq "$first declines" "$(call POST "/asks/$ask/decline" "$first")" 204
  eq "the declined ask" "$(q "SELECT status FROM asks WHERE id = '$ask'")" declined
  until_true 60 pending_ask_for "$req" > /dev/null || fail "no second driver asked within 60s of the decline: $(call GET /requests/mine P1 >/dev/null; body)"
  ask=$(pending_ask_for "$req"); second=$(name_of "$(owner_of "$ask")")
  check "the next driver is another one" "$([ "$second" != "$first" ] && echo 0 || echo 1)" "second offer to $second (ask $ask)"
  call GET /requests/mine P1 > /dev/null
  eq "P1's view after one decline: drivers_asked/declined" "$(body | j "d.requests[0].drivers_asked + '/' + d.requests[0].drivers_declined")" "2/1"

  # Expiry: the offer lasts 30 minutes (asks.DefaultTTL). Waiting that long is
  # not a test, so the ask's deadline is moved to a second ago — the only write
  # this script makes to the database — and the sweeper does the rest.
  q "UPDATE asks SET expires_at = now() - interval '1 second' WHERE id = '$ask'" > /dev/null
  ok "second offer's expires_at moved into the past (faked: the real TTL is 30 minutes)"
  eq "accepting after the deadline" "$(call POST "/asks/$ask/accept" "$second")" 409
  eq "its code" "$(err_code)" ask_expired
  until_true 90 sql_is "SELECT status FROM asks WHERE id = '$ask'" expired || fail "the sweeper did not expire the ask in 90s: $(q "SELECT status FROM asks WHERE id = '$ask'")"
  ok "the sweeper marked it expired"
  until_true 90 pending_ask_for "$req" > /dev/null || fail "no third driver asked after the expiry"
  ask=$(pending_ask_for "$req"); third=$(name_of "$(owner_of "$ask")")
  check "the third offer goes to the driver not yet asked" "$([ "$third" != "$first" ] && [ "$third" != "$second" ] && echo 0 || echo 1)" "$first, $second, now $third"
  must_eq "$third declines" "$(call POST "/asks/$ask/decline" "$third")" 204
  sleep 6
  call GET /requests/mine P1 > /dev/null
  ok "after all three: $(body | j "JSON.stringify(d.requests[0])")"
  eq "asks pending after everybody passed" "$(q "SELECT count(*) FROM asks WHERE request_id = '$req' AND status = 'pending'")" 0
  eq "a driver asked twice about one departure" "$(q "SELECT count(*) FROM (SELECT occurrence_id FROM asks WHERE request_id = '$req' GROUP BY 1 HAVING count(*) > 1) x")" 0
  eq "the request is still open" "$(q "SELECT status FROM ride_requests WHERE id = '$req'")" open
  eq "pushes queued for the three offers" "$(q "SELECT count(*) FROM outbox o JOIN asks a ON o.idempotency_key = 'ask:' || a.id WHERE a.request_id = '$req'")" 3
}

# ---- 8 ---------------------------------------------------------------------------

s8_driver_cancels() {
  local d occ who ask p3ask p1 p2 b1 b2 rows
  d=$(day 8)
  publish D "$d" 08:00 08:30 3 20
  occ=$R_OCC
  # P3 is asked first and left waiting; P1 and P2 are asked and seated one after
  # the other (a second offer already out would not survive the first accept: see
  # scenarios 5 and 6).
  ride P3 "$d" "$(along "$occ" 0.5)" "$(along "$occ" 0.9)" 1
  [ -n "$R_ASK" ] || fail "P3 not asked: $(body)"; p3ask=$R_ASK
  ride P1 "$d" "$(along "$occ" 0.05)" "$(along "$occ" 0.5)" 1
  [ -n "$R_ASK" ] || fail "P1 not asked: $(body)"; accept D "$R_ASK"; must_eq "P1 aboard" "$R_HTTP" 200
  ride P2 "$d" "$(along "$occ" 0.2)" "$(along "$occ" 0.7)" 1
  [ -n "$R_ASK" ] || fail "P2 not asked: $(body)"; accept D "$R_ASK"; must_eq "P2 aboard" "$R_HTTP" 200
  ok "P1 and P2 aboard; P3's offer ($p3ask) still pending, quoted before they came: $(q "SELECT status FROM asks WHERE id = '$p3ask'")"
  b1=$(q "SELECT id FROM bookings WHERE occurrence_id = '$occ' AND passenger_id = '$(uid_of P1)'")
  b2=$(q "SELECT id FROM bookings WHERE occurrence_id = '$occ' AND passenger_id = '$(uid_of P2)'")
  [ -n "$b1" ] && [ -n "$b2" ] || fail "need two bookings to cancel; got '$b1' '$b2'"
  must_eq "D calls off the departure" "$(call DELETE "/trips/$occ" D)" 204
  eq "trip status" "$(q "SELECT status FROM trip_occurrences WHERE id = '$occ'")" cancelled
  eq "bookings" "$(q "SELECT string_agg(status, ',') FROM bookings WHERE occurrence_id = '$occ'")" "cancelled_by_driver,cancelled_by_driver"
  eq "a second cancel is the same act" "$(call DELETE "/trips/$occ" D)" 204
  eq "P1 opens the cancelled trip" "$(call GET "/trips/$occ" P1)" 404
  eq "a passenger calls off the driver's trip" "$(call DELETE "/trips/$occ" P2)" 404
  call GET /trips/upcoming P1 > /dev/null
  eq "P1's upcoming list" "$(body | j "d.trips.filter(t => t.id === '$occ').length")" 0

  for who in P1 P2; do
    local b; b=$([ $who = P1 ] && echo "$b1" || echo "$b2")
    until_true 60 sql_is "SELECT count(*) FROM outbox WHERE payload->>'booking_id' = '$b' AND payload->>'event' = 'booking_cancelled_for_passenger' AND done_at IS NOT NULL" 1 \
      || soft "$who: the cancellation push was not delivered in 60s: $(q "SELECT id || ' ' || kind || ' attempts=' || attempts || ' done=' || coalesce(done_at::text,'-') || ' dead=' || coalesce(dead_at::text,'-') || ' err=' || coalesce(last_error,'-') FROM outbox WHERE payload->>'booking_id' = '$b'")"
    rows=$(q "SELECT string_agg(kind || '/' || coalesce(payload->>'event', payload->>'reason', '-') || ' done=' || (done_at IS NOT NULL) || ' dead=' || (dead_at IS NOT NULL), '; ') FROM outbox WHERE payload->>'booking_id' = '$b'")
    ok "$who outbox rows for booking $b: $rows"
    eq "$who has a push row" "$(q "SELECT count(*) FROM outbox WHERE payload->>'booking_id' = '$b' AND payload->>'event' = 'booking_cancelled_for_passenger'")" 1
  done
  # Reopened: the requests are back in the queue as stranded, with no car left.
  for who in P1 P2; do
    until_true 60 sql_isnt "SELECT status FROM ride_requests WHERE passenger_id = '$(uid_of $who)' AND window_start >= '$(utc "$d" 00:00)' ORDER BY created_at DESC LIMIT 1" matched \
      || soft "$who: request still matched after the driver cancelled"
    call GET /requests/mine "$who" > /dev/null
    ok "$who /requests/mine: status=$(body | j 'd.requests[0] && d.requests[0].status') stranded=$(body | j 'd.requests[0] && d.requests[0].stranded') asking_now=$(body | j 'd.requests[0] && d.requests[0].asking_now')"
    check "$who is marked stranded" "$(body | j "d.requests.length && d.requests[0].stranded ? 0 : 1")" "the request came back open with the 'search restarted' flag"
  done
  # P3's offer was for the departure that is gone.
  ok "P3's pending offer after the cancel: ask $p3ask is $(q "SELECT status FROM asks WHERE id = '$p3ask'"), request $(q "SELECT status FROM ride_requests WHERE id = '$(q "SELECT request_id FROM asks WHERE id = '$p3ask'")'")"
  call GET /requests/mine P3 > /dev/null
  ok "P3 /requests/mine: asking_now=$(body | j 'd.requests[0] && d.requests[0].asking_now') ask_expires_at=$(body | j 'd.requests[0] && d.requests[0].ask_expires_at') status=$(body | j 'd.requests[0] && d.requests[0].status')"
  call GET /asks/pending D > /dev/null
  ok "D /asks/pending lists the dead offer: $(body | j "d.asks.some(a => a.id === '$p3ask')")"
  check "no live offer left on a cancelled departure" "$([ "$(q "SELECT count(*) FROM asks WHERE occurrence_id = '$occ' AND status = 'pending' AND expires_at > now()")" = 0 ] && echo 0 || echo 1)" "pending asks on the cancelled departure: $(q "SELECT count(*) FROM asks WHERE occurrence_id = '$occ' AND status = 'pending'")"
  eq "D accepting P3's offer on the cancelled departure" "$(call POST "/asks/$p3ask/accept" D)" 409
}

# ---- 9 ---------------------------------------------------------------------------

s9_route_edit() {
  local d wd tpl occ0 occs now_e edit
  d=$(day 6); wd=$(TZ=Europe/Athens date -d "$d" +%u)
  local resp
  resp=$(curl -s -X POST "$API/routes" -H "Authorization: Bearer $(tok D)" -H 'Content-Type: application/json' -d "{
    \"origin_lon\":25.8743,\"origin_lat\":40.8476,\"destination_lon\":25.4048,\"destination_lat\":41.1224,
    \"origin_label\":\"Αλεξανδρούπολη\",\"destination_label\":\"Κομοτηνή\",\"seats\":3,\"detour_min\":10,
    \"kind\":\"recurring\",\"weekdays\":[$wd],\"depart_earliest\":\"08:00\",\"depart_latest\":\"08:30\"}")
  tpl=$(printf '%s' "$resp" | j "d.template_id || ''"); [ -n "$tpl" ] || fail "recurring publish: $resp"
  occs=$(q "SELECT string_agg(to_char(depart_earliest AT TIME ZONE 'Europe/Athens', 'MM-DD HH24:MI'), ', ' ORDER BY depart_earliest) FROM trip_occurrences WHERE template_id = '$tpl'")
  ok "D publishes every $(TZ=Europe/Athens date -d "$d" +%A) 08:00; departures: $occs"
  eq "departures planned" "$(q "SELECT count(*) FROM trip_occurrences WHERE template_id = '$tpl'")" 3
  occ0=$(q "SELECT id FROM trip_occurrences WHERE template_id = '$tpl' ORDER BY depart_earliest LIMIT 1")
  ride P1 "$d" "$(along "$occ0" 0.05)" "$(along "$occ0" 0.6)" 1
  [ -n "$R_ASK" ] || fail "P1 not asked: $(body)"
  accept D "$R_ASK"; must_eq "P1 booked on the first departure" "$R_HTTP" 200

  edit="{\"origin_lon\":25.8743,\"origin_lat\":40.8476,\"destination_lon\":25.4048,\"destination_lat\":41.1224,
    \"origin_label\":\"Αλεξανδρούπολη\",\"destination_label\":\"Κομοτηνή\",\"seats\":3,\"detour_min\":10,
    \"kind\":\"recurring\",\"weekdays\":[$wd],\"depart_earliest\":\"09:00\",\"depart_latest\":\"09:30\"}"
  must_eq "D moves the route to 09:00" "$(call PUT "/me/routes/$tpl" D "$edit")" 200
  eq "kept_trips in the reply" "$(body | j 'd.kept_trips')" 1
  sleep 2
  eq "the booked departure keeps 08:00" "$(q "SELECT to_char(depart_earliest AT TIME ZONE 'Europe/Athens', 'HH24:MI') FROM trip_occurrences WHERE id = '$occ0'")" 08:00
  eq "the other departures take 09:00" "$(q "SELECT string_agg(DISTINCT to_char(depart_earliest AT TIME ZONE 'Europe/Athens', 'HH24:MI'), ',') FROM trip_occurrences WHERE template_id = '$tpl' AND id <> '$occ0' AND status = 'scheduled'")" 09:00
  eq "departures after the edit" "$(q "SELECT count(*) FROM trip_occurrences WHERE template_id = '$tpl' AND status = 'scheduled'")" 3
  call GET "/trips/$occ0" P1 > /dev/null
  eq "P1's trip card still says 08:00" "$(TZ=Europe/Athens date -d "$(body | j 'd.depart_earliest')" +%H:%M)" 08:00
  eq "P1's seat" "$(q "SELECT status FROM bookings WHERE occurrence_id = '$occ0' AND passenger_id = '$(uid_of P1)'")" confirmed
  # Seats on a booked departure are not rewritten either.
  must_eq "D adds a seat to the route" "$(call PUT "/me/routes/$tpl" D "${edit/\"seats\":3/\"seats\":5}")" 200
  eq "booked departure's seats" "$(q "SELECT seats_total || '/' || seats_free FROM trip_occurrences WHERE id = '$occ0'")" "3/2"
  eq "another departure's seats" "$(q "SELECT DISTINCT seats_total FROM trip_occurrences WHERE template_id = '$tpl' AND id <> '$occ0' AND status = 'scheduled'")" 5
  eq "another driver edits the route" "$(call PUT "/me/routes/$tpl" D2 "$edit")" 404
  eq "moving an end" "$(call PUT "/me/routes/$tpl" D "${edit/25.4048/25.30}")" 409
  eq "its code" "$(err_code)" route_ends_changed
}

# ---- 12 --------------------------------------------------------------------------

s12_other_days() {
  local days=() occs=() reqs=() asks=() i root
  for i in 9 10 11; do days+=("$(day $i)"); done
  for i in 0 1 2; do publish D2 "${days[$i]}" 08:00 08:30 3 15; occs+=("$R_OCC"); done
  ok "D2 drives on ${days[*]}"
  local from to
  for i in 0 1 2; do
    from=$(along "${occs[$i]}" 0.05); to=$(along "${occs[$i]}" 0.6)
    ride P4 "${days[$i]}" "$from" "$to" 1 "" "" "$([ "$i" = 0 ] || echo "$root")"
    must_eq "P4's request for ${days[$i]}" "$R_HTTP" 201
    reqs+=("$R_REQ"); asks+=("$R_ASK")
    [ "$i" != 0 ] || root=$R_REQ
    [ -n "$R_ASK" ] || fail "P4 not asked for ${days[$i]}: $(body)"
  done
  eq "three requests, one group" "$(q "SELECT count(*) FROM ride_requests WHERE COALESCE(alternative_of, id) = '$root'")" 3
  eq "three offers pending" "$(q "SELECT count(*) FROM asks WHERE id IN ('${asks[0]}','${asks[1]}','${asks[2]}') AND status = 'pending'")" 3
  eq "a stranger adds a day to P4's journey" "$(call POST /requests X "{\"origin_lon\":25.86,\"origin_lat\":40.87,\"destination_lon\":25.5,\"destination_lat\":41.05,\"origin_label\":\"a\",\"destination_label\":\"b\",\"seats\":1,\"window_start\":\"$(utc "$(day 14)" 07:00)\",\"window_end\":\"$(utc "$(day 14)" 10:00)\",\"alternative_of\":\"$root\"}")" 404
  eq "alternative_of that is not an id" "$(call POST /requests P4 '{"alternative_of":"nope","origin_lon":25.86,"origin_lat":40.87,"destination_lon":25.5,"destination_lat":41.05,"origin_label":"a","destination_label":"b"}')" 400

  must_eq "D2 accepts the middle day" "$(call POST "/asks/${asks[1]}/accept" D2)" 200
  eq "the day taken" "$(q "SELECT status FROM ride_requests WHERE id = '${reqs[1]}'")" matched
  eq "the other two requests" "$(q "SELECT string_agg(status, ',') FROM ride_requests WHERE id IN ('${reqs[0]}','${reqs[2]}')")" "cancelled,cancelled"
  eq "the three asks (accepted, withdrawn, withdrawn)" "$(q "SELECT string_agg(status, ',' ORDER BY status) FROM asks WHERE id IN ('${asks[0]}','${asks[1]}','${asks[2]}')")" "accepted,cancelled,cancelled"
  eq "D2 accepting a withdrawn day" "$(call POST "/asks/${asks[0]}/accept" D2)" 409
  eq "its code" "$(err_code)" ask_closed
  call GET /requests/mine P4 > /dev/null
  eq "P4's open requests" "$(body | j 'd.requests.length')" 0
  eq "D2's pending asks" "$(call GET /asks/pending D2 >/dev/null; body | j 'd.asks.length')" 0
  eq "bookings P4 holds" "$(q "SELECT count(*) FROM bookings WHERE passenger_id = '$(uid_of P4)' AND status = 'confirmed'")" 1
}

# ---- 13 --------------------------------------------------------------------------

s13_refusals() {
  local d occ o e req mine bad
  d=$(day 12)
  publish D3 "$d" 08:00 08:30 3 15
  occ=$R_OCC
  o=$(along "$occ" 0.05); e=$(along "$occ" 0.6)
  local op=($o) ep=($e)
  post() { # seats start end [extra] -> code
    call POST /requests P5 "{\"origin_lon\":${op[0]},\"origin_lat\":${op[1]},\"destination_lon\":${ep[0]},\"destination_lat\":${ep[1]},
      \"origin_label\":\"Στον δρόμο\",\"destination_label\":\"Στον δρόμο\",\"seats\":$1,\"window_start\":\"$2\",\"window_end\":\"$3\"${4:-}}"
  }
  local ws we; ws=$(utc "$d" 07:00); we=$(utc "$d" 10:00)
  eq "seats=5 (car has 3)" "$(post 5 "$ws" "$we")" 400; eq "  code" "$(err_code)" invalid_seats
  eq "seats=0" "$(post 0 "$ws" "$we")" 400
  eq "seats=-1" "$(post -1 "$ws" "$we")" 400
  # Four is a party the form allows; the only car on the road has three.
  eq "seats=4 on a road whose cars have 3" "$(post 4 "$ws" "$we")" 201
  req=$(body | j 'd.request_id'); eq "  asked" "$(body | j 'd.asking')" false
  call GET /requests/mine P5 > /dev/null
  mine=$(body | j "JSON.stringify(d.requests.find(r => r.id === '$req'))")
  ok "  /requests/mine says: $mine"
  check "  told that no car has four seats" "$(printf '%s' "$mine" | j "d.most_seats_on_corridor != null && d.most_seats_on_corridor < d.seats ? 0 : 1")" "most_seats_on_corridor=$(printf '%s' "$mine" | j 'd.most_seats_on_corridor') seats=4"
  call DELETE "/requests/$req" P5 > /dev/null

  eq "window already over" "$(post 1 "$(date -u -d '-3 hours' +%FT%TZ)" "$(date -u -d '-1 hour' +%FT%TZ)")" 400; eq "  code" "$(err_code)" window_in_past
  eq "window 22 days ahead" "$(post 1 "$(utc "$(day 22)" 07:00)" "$(utc "$(day 22)" 10:00)")" 400; eq "  code" "$(err_code)" window_too_far
  eq "window ending before it starts" "$(post 1 "$we" "$ws")" 400
  eq "window of zero length" "$(post 1 "$ws" "$ws")" 400
  eq "origin == destination" "$(call POST /requests P5 "{\"origin_lon\":${op[0]},\"origin_lat\":${op[1]},\"destination_lon\":${op[0]},\"destination_lat\":${op[1]},\"origin_label\":\"a\",\"destination_label\":\"a\",\"seats\":1,\"window_start\":\"$ws\",\"window_end\":\"$we\"}")" 400; eq "  code" "$(err_code)" too_short
  eq "a destination in Turkey" "$(call POST /requests P5 "{\"origin_lon\":${op[0]},\"origin_lat\":${op[1]},\"destination_lon\":28.97,\"destination_lat\":41.01,\"origin_label\":\"a\",\"destination_label\":\"b\",\"seats\":1,\"window_start\":\"$ws\",\"window_end\":\"$we\"}")" 400
  eq "an unknown luggage size" "$(post 1 "$ws" "$we" ',"luggage":"piano","luggage_count":1')" 400
  eq "bulky, none of them" "$(post 1 "$ws" "$we" ',"luggage":"bulky","luggage_count":0')" 400
  eq "five bags" "$(post 1 "$ws" "$we" ',"luggage":"cabin","luggage_count":5')" 400
  eq "no bags, three of them" "$(post 1 "$ws" "$we" ',"luggage":"none","luggage_count":3')" 400
  eq "no token" "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/requests" -H 'Content-Type: application/json' -d '{}')" 401
  eq "garbage body" "$(call POST /requests P5 'not json')" 400

  # Luggage the car cannot take: bulky on a route that takes a suitcase at most.
  eq "bulky on a suitcase car" "$(post 1 "$ws" "$we" ',"luggage":"bulky","luggage_count":1')" 201
  req=$(body | j 'd.request_id'); eq "  asked" "$(body | j 'd.asking')" false
  call GET /requests/mine P5 > /dev/null
  mine=$(body | j "JSON.stringify(d.requests.find(r => r.id === '$req'))")
  ok "  /requests/mine says: $mine"
  check "  told it is the luggage" "$(printf '%s' "$mine" | j "/lugg|bulky/.test(JSON.stringify(d)) || (d.missed && d.missed.reason) ? 0 : 1")" "nothing in the answer names the luggage; the app shows 'no driver has been asked yet'"
  call DELETE "/requests/$req" P5 > /dev/null
  # Room for the size but not for the number: one bag of space, two people with a suitcase.
  d=$(day 13)
  publish D2 "$d" 08:00 08:30 3 15 ',"luggage":"suitcase","luggage_capacity":1'
  occ=$R_OCC
  ride P5 "$d" "$(along "$occ" 0.05)" "$(along "$occ" 0.6)" 1 suitcase 1
  [ -n "$R_ASK" ] || fail "P5 with one suitcase not asked: $(body)"
  accept D2 "$R_ASK"; must_eq "P5's suitcase takes the boot" "$R_HTTP" 200
  ride P6 "$d" "$(along "$occ" 0.2)" "$(along "$occ" 0.7)" 1 suitcase 1
  eq "P6's suitcase, boot full: asked" "${R_ASK:-none}" none
  call GET /requests/mine P6 > /dev/null
  mine=$(body | j "JSON.stringify(d.requests[0])")
  ok "  /requests/mine says: $mine"
  check "  told the boot is full" "$(printf '%s' "$mine" | j "/lugg|boot|bag/.test(JSON.stringify(d)) || (d.missed && d.missed.reason) ? 0 : 1")" "nothing in the answer names the luggage"
  ride X "$d" "$(along "$occ" 0.2)" "$(along "$occ" 0.7)" 1
  [ -n "$R_ASK" ] && ok "a passenger with no luggage is asked (seats free: $(seats_free "$occ"))" || soft "a passenger with no luggage was not asked on a car with 2 free seats: $(body)"
}

# ---- 14 --------------------------------------------------------------------------

# Everything this run caused to be queued for these accounts has been delivered
# or is on its way: nothing dead, nothing stuck.
s14_outbox() {
  local ids pat
  ids=$(q "SELECT string_agg(id::text, '|') FROM users WHERE phone IN ($(for n in "${NAMES[@]}"; do printf "'%s'," "$(phone_of "$n")"; done | sed 's/,$//'))")
  pat="($ids)"
  until_true 60 sql_is "SELECT count(*) FROM outbox WHERE created_at >= '$START' AND done_at IS NULL AND dead_at IS NULL AND run_after < now() - interval '1 minute' AND payload::text ~ '$pat'" 0 \
    || soft "rows overdue for over a minute: $(q "SELECT kind || ':' || idempotency_key FROM outbox WHERE created_at >= '$START' AND done_at IS NULL AND dead_at IS NULL AND run_after < now() - interval '1 minute' AND payload::text ~ '$pat' LIMIT 5")"
  eq "rows queued for these accounts during the run" "$(q "SELECT count(*) > 0 FROM outbox WHERE created_at >= '$START' AND payload::text ~ '$pat'")" t
  eq "dead-lettered rows" "$(q "SELECT count(*) FROM outbox WHERE created_at >= '$START' AND dead_at IS NOT NULL AND payload::text ~ '$pat'")" 0
  ok "by kind: $(q "SELECT string_agg(kind || '=' || n || ' (done ' || d || ')', ', ') FROM (SELECT kind, count(*) n, count(done_at) d FROM outbox WHERE created_at >= '$START' AND payload::text ~ '$pat' GROUP BY kind ORDER BY kind) x")"
  eq "rows that needed more than one attempt" "$(q "SELECT count(*) FROM outbox WHERE created_at >= '$START' AND attempts > 1 AND payload::text ~ '$pat'")" 0
  eq "asks left pending on a departure that is cancelled" "$(q "SELECT count(*) FROM asks a JOIN trip_occurrences o ON o.id = a.occurrence_id WHERE a.created_at >= '$START' AND a.status = 'pending' AND o.status = 'cancelled' AND a.expires_at > now()")" 0
  eq "seats_free below zero or above the car" "$(q "SELECT count(*) FROM trip_occurrences WHERE updated_at >= '$START' AND (seats_free < 0 OR seats_free > seats_total)")" 0
  eq "seats_free that does not match the bookings (confirmed departures only)" "$(q "SELECT count(*) FROM trip_occurrences o WHERE o.updated_at >= '$START' AND o.status = 'scheduled' AND o.seats_total - o.seats_free <> COALESCE((SELECT sum(seats) FROM bookings b WHERE b.occurrence_id = o.id AND b.status IN ('confirmed','completed')), 0) AND EXISTS (SELECT 1 FROM route_templates t JOIN users u ON u.id = t.driver_id WHERE t.id = o.template_id AND u.phone IN ($(for n in "${NAMES[@]}"; do printf "'%s'," "$(phone_of "$n")"; done | sed 's/,$//')))")" 0
}

# ---- runner -------------------------------------------------------------------------

declare -A TITLE=(
  [1]="first passenger asked and accepted"
  [2]="party of two, itinerary, who sees whom"
  [3]="car full: nobody asked, and what P3 is told"
  [4]="cancel wakes the waiting passenger"
  [5]="party cap"
  [6]="races for the last seat"
  [7]="decline and expiry move on"
  [8]="driver calls off the departure"
  [9]="route edit with a booked departure"
  [10]="chat"
  [11]="live location"
  [12]="same journey on three days"
  [13]="refusals at the door"
  [14]="the outbox and the seat counts after the run"
)
declare -A FN=(
  [1]=s1_first_passenger [2]=s2_party_of_two [3]=s3_full_car [4]=s4_cancel_wakes_waiter
  [5]=s5_party_cap [6]=s6_races [7]=s7_decline_expiry [8]=s8_driver_cancels
  [9]=s9_route_edit [10]=s10_chat [11]=s11_live [12]=s12_other_days [13]=s13_refusals [14]=s14_outbox
)
RESULTS=()
START=$(q "SELECT now()")

run_scenario() { # n
  local n=$1 rc=0
  step "$n. ${TITLE[$n]}"
  rm -f "$TMP/softfail"
  # 2, 3, 4, 10 and 11 go on with the car 1 set up; every other scenario starts
  # from accounts with nothing booked, asked or published.
  case $n in 2|3|4|10|11|14) ;; *) reset_all;; esac
  if ! declare -F "${FN[$n]}" > /dev/null; then
    printf '   skipped: not written\n'; RESULTS+=("$n|SKIP|"); return
  fi
  set +e
  ( set -eE; trap 'printf "\033[31m   FAIL: line %s: %s\033[0m\n" "$LINENO" "$BASH_COMMAND"; echo "script error line $LINENO: $BASH_COMMAND" >> "$TMP/softfail"' ERR; ${FN[$n]} )
  rc=$?
  set -e
  if [ "$rc" != 0 ] || [ -s "$TMP/softfail" ]; then
    RESULTS+=("$n|FAIL|$(head -c 300 "$TMP/softfail" 2>/dev/null | tr '\n' ';')")
  else
    RESULTS+=("$n|PASS|")
  fi
}

main() {
  local n name
  step "accounts (block RUN=$RUN, phones $(phone_of D) .. $(phone_of X))"
  for name in "${NAMES[@]}"; do login "$name"; call PATCH /me "$name" "{\"name\":\"QT $name\"}" > /dev/null; done
  for name in D D2 D3; do vehicle_for "$name"; done
  reset_all
  # The other blocks' accounts, with whatever an earlier run left standing.
  local f ph
  for f in "$TOKDIR"/+*.ref; do
    ph=$(basename "$f" .ref)
    case " $(for name in "${NAMES[@]}"; do phone_of "$name"; done | tr '\n' ' ') " in *" $ph "*) continue;; esac
    refresh_tokens "$ph" && reset_account "$ph" || true
  done
  ok "signed in and cleared"

  local want=("$@")
  [ ${#want[@]} -gt 0 ] || want=(1 2 3 4 10 11 5 6 7 8 9 12 13 14)
  for n in "${want[@]}"; do run_scenario "$n"; done

  step "summary"
  local fails=0 r
  while IFS='|' read -r n verdict why; do
    printf '  %2s  %-5s %s  %s\n' "$n" "$verdict" "${TITLE[$n]}" "$why"
    [ "$verdict" = FAIL ] && fails=$((fails + 1))
  done < <(printf '%s\n' "${RESULTS[@]}" | sort -n)
  [ "$fails" = 0 ] || { printf '\033[31m%d scenario(s) FAILed\033[0m\n' "$fails"; exit 1; }
  printf '\033[32mall scenarios passed\033[0m\n'
}
main "$@"

#!/usr/bin/env bash
# QA scenario: a drive, end to end, against a running API — specs/DRIVING-NOW.md.
#
# The unit tests pin each rule with a fake clock and a straight-line router.
# This runs the whole thing the way it happens: the real clock, real OSRM, the
# outbox dispatcher and the nudge job doing their work on their own schedule.
#
#   IP=127.0.0.1 API_PORT=8187 WEB_PORT=8197 DB=qa scripts/dev-lan.sh
#   API=http://127.0.0.1:8187/v1 scripts/seed-demo.sh
#   API=http://127.0.0.1:8187/v1 DSN=postgres://carpool:carpool@localhost:5433/qa?sslmode=disable \
#     scripts/qa-drive.sh
#
# 1. a nudge: a departure in a quarter of an hour, somebody on the road ready
# 2. «Φεύγω» now, a position, and the passenger is asked — with the buttons
# 3. accept, and what the driver's maps hand-off and the passenger see
# 4. «Είμαι εδώ», stop taking people, «Τέλος», and a late upload refused
#
# RUN distinguishes one run from the next in the phone numbers it signs up.
set -euo pipefail

API=${API:-http://127.0.0.1:8187/v1}
DSN=${DSN:-postgres://carpool:carpool@localhost:5433/qa?sslmode=disable}
RUN=${RUN:-$(date +%M%S | cut -c1-4)}
HOME_PLACE=node/139260653
DOB=1990-01-01

j() { node -pe "const d=JSON.parse(require('fs').readFileSync(0));$1"; }
step() { printf '\n\033[1m%s\033[0m\n' "$*"; }
ok() { printf '   \033[32mok\033[0m %s\n' "$*"; }
fail() { printf '\033[31m   FAIL: %s\033[0m\n' "$*"; exit 1; }
q() { psql "$DSN" -tAc "$1"; }

signup() { # phone name
  local body code verified ticket
  body=$(curl -s -X POST "$API/auth/otp/request" -H 'Content-Type: application/json' -d "{\"phone\":\"$1\"}")
  code=$(printf '%s' "$body" | j "d.dev_code || ''")
  [ -n "$code" ] || fail "no dev_code for $1: $body"
  verified=$(curl -s -X POST "$API/auth/otp/verify" -H 'Content-Type: application/json' \
    -d "{\"phone\":\"$1\",\"code\":\"$code\"}")
  ticket=$(printf '%s' "$verified" | j "d.signup_token || ''")
  if [ -z "$ticket" ]; then printf '%s' "$verified" | j "d.access_token"; return; fi
  curl -s -X POST "$API/auth/signup/complete" -H 'Content-Type: application/json' \
    -d "{\"signup_token\":\"$ticket\",\"name\":\"$2\",\"home_place_id\":\"$HOME_PLACE\",\"date_of_birth\":\"$DOB\"}" \
    | j "d.access_token"
}
call() { # method path token [body]
  curl -s -o /tmp/qa-drive.body -w '%{http_code}' -X "$1" "$API$2" -H "Authorization: Bearer $3" \
    -H 'Content-Type: application/json' ${4:+-d "$4"}
}
become_driver() { # token plate
  curl -s -X POST "$API/me/vehicles" -H "Authorization: Bearer $1" -H 'Content-Type: application/json' \
    -d "{\"make\":\"Toyota\",\"model\":\"Yaris\",\"fuel\":\"petrol\",\"engine_cc\":1330,
         \"colour\":\"ασημί\",\"plate\":\"$2\",\"label\":\"το αμάξι\"}" > /dev/null
}
hm() { TZ=Europe/Athens date -d "$1" +%H:%M; }
TODAY=$(TZ=Europe/Athens date +%Y-%m-%d)
publish() { # token earliest latest -> template id
  curl -s -X POST "$API/routes" -H "Authorization: Bearer $1" -H 'Content-Type: application/json' -d "{
    \"origin_lon\":25.8743,\"origin_lat\":40.8476,\"destination_lon\":26.1720,\"destination_lat\":40.8960,
    \"origin_label\":\"Αλεξανδρούπολη\",\"destination_label\":\"Φέρες\",\"seats\":3,
    \"kind\":\"oneoff\",\"oneoff_date\":\"$TODAY\",\"depart_earliest\":\"$2\",\"depart_latest\":\"$3\"}" \
    | j "d.template_id || JSON.stringify(d)"
}
occurrence_of() { q "SELECT id FROM trip_occurrences WHERE template_id = '$1' ORDER BY depart_earliest LIMIT 1"; }
# A point a fraction of the way along a departure's road, as lon lat.
along() { q "SELECT ST_X(p) || ' ' || ST_Y(p) FROM (SELECT ST_LineInterpolatePoint(route_geom, $2) p FROM trip_occurrences WHERE id = '$1') x"; }
ask_ride() { # token from(lon lat) to(lon lat) -> request id
  local o=($2) d=($3)
  curl -s -X POST "$API/requests" -H "Authorization: Bearer $1" -H 'Content-Type: application/json' -d "{
    \"origin_lon\":${o[0]},\"origin_lat\":${o[1]},\"destination_lon\":${d[0]},\"destination_lat\":${d[1]},
    \"origin_label\":\"Στον δρόμο\",\"destination_label\":\"Πριν τις Φέρες\",
    \"window_start\":\"$(date -u -d '+1 minute' +%FT%TZ)\",\"window_end\":\"$(date -u -d '+2 hours' +%FT%TZ)\"}" \
    | j "d.request_id || JSON.stringify(d)"
}

step "1. a nudge: a departure in a quarter of an hour, and somebody on the road ready"
N=$(signup "+3069001${RUN}1" "Οδηγός νύξης")
become_driver "$N" "NN${RUN}-1111"
NT=$(publish "$N" "$(hm '+12 minutes')" "$(hm '+40 minutes')")
NO=$(occurrence_of "$NT")
[ -n "$NO" ] || fail "the one-off departure was not materialized"
[ "$(call PUT "/me/routes/$NT/nudge" "$N" '{"on":true}')" = 204 ] || fail "turning the nudge on"
P1=$(signup "+3069001${RUN}2" "Επιβάτης νύξης")
R1=$(ask_ride "$P1" "$(along "$NO" 0.6)" "$(along "$NO" 0.95)")
echo "   departure $NO, request $R1; waiting for the nudge job (it runs every minute)"
for _ in $(seq 1 30); do
  [ "$(q "SELECT count(*) FROM outbox WHERE idempotency_key = 'drive_nudge:$NO'")" = 1 ] && break
  sleep 3
done
[ "$(q "SELECT count(*) FROM outbox WHERE idempotency_key = 'drive_nudge:$NO'")" = 1 ] || fail "no nudge queued"
ok "nudge queued: $(q "SELECT payload FROM outbox WHERE idempotency_key = 'drive_nudge:$NO'")"

step "2. «Φεύγω» now, a position, and the passenger is asked"
D=$(signup "+3069002${RUN}1" "Οδηγός")
become_driver "$D" "DD${RUN}-2222"
DT=$(publish "$D" "$(hm '+20 minutes')" "$(hm '+50 minutes')")
DO=$(occurrence_of "$DT")
NOW=$(date -u +%FT%TZ)
[ "$(call POST "/trips/$DO/drive" "$D" "{\"departs_at\":\"$NOW\",\"detour_min\":5}")" = 200 ] \
  || fail "«Φεύγω»: $(cat /tmp/qa-drive.body)"
ok "pinned to $(cat /tmp/qa-drive.body)"
START=($(along "$DO" 0.02))
[ "$(call PUT "/trips/$DO/position" "$D" "{\"lat\":${START[1]},\"lon\":${START[0]},\"accuracy_m\":15,\"observed_at\":\"$(date -u +%FT%TZ)\"}")" = 204 ] \
  || fail "the car's position: $(cat /tmp/qa-drive.body)"
ok "the car is at the start of the road"
P2=$(signup "+3069002${RUN}2" "Επιβάτης")
R2=$(ask_ride "$P2" "$(along "$DO" 0.6)" "$(along "$DO" 0.95)")
ASK=""
for _ in $(seq 1 20); do
  ASK=$(q "SELECT id FROM asks WHERE request_id = '$R2' AND occurrence_id = '$DO' AND status = 'pending'")
  [ -n "$ASK" ] && break
  sleep 2
done
[ -n "$ASK" ] || fail "the passenger was not asked about the drive (request $R2: $(q "SELECT status FROM ride_requests WHERE id = '$R2'"))"
TTL=$(q "SELECT round(extract(epoch FROM expires_at - created_at)) FROM asks WHERE id = '$ASK'")
[ "$TTL" -le 125 ] || fail "an offer on a moving drive runs ${TTL}s, want about two minutes"
ok "asked, with ${TTL}s to answer"
PUSH=$(q "SELECT payload->>'drive' FROM outbox WHERE idempotency_key = 'ask:$ASK'")
[ "$PUSH" = true ] || fail "the offer's push is not marked as a drive's: $PUSH"
ok "the offer's push carries the drive buttons"

step "3. accept, and what the driver's maps hand-off and the passenger see"
[ "$(call POST "/asks/$ASK/accept" "$D")" = 200 ] || fail "accept: $(cat /tmp/qa-drive.body)"
BOOKING=$(j "d.booking_id" < /tmp/qa-drive.body)
call GET "/trips/$DO" "$D" > /dev/null
SEAT=$(j "JSON.stringify(d.passengers.find(p => p.booking_id === '$BOOKING') || null)" < /tmp/qa-drive.body)
[ "$SEAT" != null ] || fail "the driver's trip does not list the new seat by booking id"
j "d.destination && d.destination.length === 2 ? 'ok' : process.exit(1)" < /tmp/qa-drive.body > /dev/null \
  || fail "the driver's trip has no destination"
ok "the driver has the seat, its pickup and drop-off, and the end of the road: $SEAT"
call GET "/trips/$DO" "$P2" > /dev/null
PICKUP_AT=$(j "d.pickup_at || ''" < /tmp/qa-drive.body)
[ -n "$PICKUP_AT" ] || fail "the passenger has no pickup time"
j "d.destination ? process.exit(1) : 'ok'" < /tmp/qa-drive.body > /dev/null || fail "the passenger was given the driver's destination"
ok "the passenger is picked up around $PICKUP_AT, and sees nothing of the driver's"

step "4. «Είμαι εδώ», stop taking people, «Τέλος», and a late upload refused"
[ "$(call POST "/trips/$DO/here" "$P2")" = 204 ] || fail "«Είμαι εδώ»: $(cat /tmp/qa-drive.body)"
[ "$(q "SELECT count(*) FROM outbox WHERE idempotency_key = 'passenger_here:$BOOKING'")" = 1 ] || fail "the driver was not told"
ok "the driver is told the passenger is at the pickup"
[ "$(call PATCH "/trips/$DO/drive" "$D" '{"offering":false}')" = 204 ] || fail "stop taking people"
[ "$(q "SELECT offers_until <= now() FROM trip_occurrences WHERE id = '$DO'")" = t ] || fail "still offering"
ok "no longer taking people"
[ "$(call POST "/trips/$DO/drive/end" "$D")" = 204 ] || fail "«Τέλος»"
CODE=$(call PUT "/trips/$DO/position" "$D" "{\"lat\":${START[1]},\"lon\":${START[0]},\"accuracy_m\":15}")
[ "$CODE" = 409 ] || fail "an upload after «Τέλος» answered $CODE"
[ "$(q "SELECT count(*) FROM trip_live_positions WHERE occurrence_id = '$DO' AND user_id = (SELECT id FROM users WHERE phone = '+3069002${RUN}1')")" = 0 ] \
  || fail "the car's position outlived «Τέλος»"
ok "«Τέλος»: the position is gone, and a late upload is refused with 409"

DEAD=$(q "SELECT count(*) FROM outbox WHERE dead_at IS NOT NULL AND idempotency_key LIKE ANY (ARRAY['drive_%$NO%','ask:$ASK','passenger_here:$BOOKING'])")
[ "$DEAD" = 0 ] || fail "$DEAD of this run's notices were dead-lettered"
step "all good"

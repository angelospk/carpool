#!/usr/bin/env bash
# Adversarial QA of the carpool API: authorization, data exposure between
# passengers, state-machine abuse, concurrency, auth. Probes only; it fixes
# nothing and asserts the SECURE behaviour, so every FAIL line is a finding.
#
#   API=http://127.0.0.1:8188/v1 \
#   DSN='postgres://carpool:carpool@localhost:5433/qa1017api?sslmode=disable' \
#     scripts/qa-adversarial.sh
#
# Needs a DEV_MODE server (the OTP code comes back in the response), curl, jq,
# psql (read-only use) and node >= 22 (global WebSocket, for the chat-socket check). Takes about 6 minutes: it publishes a trip that leaves
# in LEAD_MIN minutes and waits for it to be over before it tests rating,
# confirming and expired asks.
#
# Each run signs up nine fresh phones (+30690000 3000..3099, cycling) and
# spreads sign-ins over 127.0.0.10..250 with `curl --interface`, because the
# API allows 20 OTP codes an hour per source address. The counters live in
# $STATE (default /tmp/qa-adversarial.state); delete it to start over.
#
# Prints "ok" / "FAIL" per check and carries on after a FAIL. Exit status is
# the number of FAILs (capped at 100).
set -uo pipefail

API=${API:-http://127.0.0.1:8188/v1}
DSN=${DSN:-postgres://carpool:carpool@localhost:5433/qa1017api?sslmode=disable}
STATE=${STATE:-/tmp/qa-adversarial.state}
LEAD_MIN=${LEAD_MIN:-4}
HOME_PLACE=node/139260653
DOB=1990-01-01
TMP=$(mktemp -d /tmp/qa-adv.XXXXXX)
trap 'rm -rf "$TMP"' EXIT

PASSES=0
FAILS=0

# ---------------------------------------------------------------- helpers ---
q() { psql "$DSN" -tAc "$1" 2>&1; }
j() { jq -r "$1" 2>/dev/null; }
section() { printf '\n== %s\n' "$*"; }
ok() { PASSES=$((PASSES + 1)); printf '  ok   %s\n' "$1"; }
bad() { FAILS=$((FAILS + 1)); printf '  FAIL %s\n         %s\n' "$1" "$2"; }
excerpt() { printf '%s' "$RES" | tr '\n' ' ' | head -c "${1:-200}"; }
note() { printf '  note %s\n' "$*"; }

# req METHOD PATH TOKEN [BODY] [extra curl args...] -> CODE, RES, LAST
req() {
  local m=$1 p=$2 t=${3-} b=${4-}
  shift 4 2>/dev/null || shift $#
  local args=(-s -m 30 -o "$TMP/res" -w '%{http_code}' -X "$m" "$API$p" -H 'Content-Type: application/json' "$@")
  [ -n "$t" ] && args+=(-H "Authorization: Bearer $t")
  [ -n "$b" ] && args+=(-d "$b")
  CODE=$(curl "${args[@]}" 2>/dev/null)
  RES=$(cat "$TMP/res" 2>/dev/null)
  LAST="$m $p"
}
# want NAME REGEX : the last response's status must match REGEX
want() {
  if [[ $CODE =~ ^($2)$ ]]; then ok "$1 [$CODE]"; else bad "$1" "$LAST -> $CODE $(excerpt)"; fi
}
# lacks NAME FIXED : the last response body must not contain FIXED
lacks() {
  if printf '%s' "$RES" | grep -qF -- "$2"; then bad "$1" "$LAST -> $CODE contains '$2': $(excerpt 300)"; else ok "$1"; fi
}
# has NAME FIXED : the last response body must contain FIXED
has() {
  if printf '%s' "$RES" | grep -qF -- "$2"; then ok "$1"; else bad "$1" "$LAST -> $CODE lacks '$2': $(excerpt 300)"; fi
}
# dbeq NAME SQL EXPECTED
dbeq() {
  local got; got=$(q "$2")
  if [ "$got" = "$3" ]; then ok "$1 [$got]"; else bad "$1" "SQL: $2 -> '$got', want '$3'"; fi
}

# counters survive between runs so sign-ins use fresh addresses and phones
[ -f "$STATE" ] || echo "9 0" > "$STATE"
next_ip() { local ip ph; read -r ip ph < "$STATE"; ip=$((ip + 1)); [ "$ip" -gt 250 ] && ip=10; echo "$ip $ph" > "$STATE"; IPADDR=127.0.0.$ip; }
next_phone() { local ip ph; read -r ip ph < "$STATE"; ph=$((ph + 1)); [ "$ph" -gt 99 ] && ph=0; echo "$ip $ph" > "$STATE"; PHONE=$(printf '+30690000%04d' $((3000 + ph))); }

# signup NAME -> TOKEN REFRESH USER_ID PHONE
signup() {
  local name=$1 body code v ticket
  next_phone; next_ip
  body=$(curl -s --interface "$IPADDR" -X POST "$API/auth/otp/request" -H 'Content-Type: application/json' -d "{\"phone\":\"$PHONE\"}")
  code=$(printf '%s' "$body" | j '.dev_code // empty')
  [ -n "$code" ] || { echo "no dev_code for $PHONE: $body" >&2; TOKEN=""; return 1; }
  v=$(curl -s -X POST "$API/auth/otp/verify" -H 'Content-Type: application/json' -d "{\"phone\":\"$PHONE\",\"code\":\"$code\"}")
  ticket=$(printf '%s' "$v" | j '.signup_token // empty')
  if [ -n "$ticket" ]; then
    v=$(curl -s -X POST "$API/auth/signup/complete" -H 'Content-Type: application/json' \
      -d "{\"signup_token\":\"$ticket\",\"name\":\"$name\",\"home_place_id\":\"$HOME_PLACE\",\"date_of_birth\":\"$DOB\"}")
  fi
  TOKEN=$(printf '%s' "$v" | j '.access_token // empty')
  REFRESH=$(printf '%s' "$v" | j '.refresh_token // empty')
  [ -n "$TOKEN" ] || { echo "signup failed for $name: $v" >&2; return 1; }
  USER_ID=$(q "SELECT id FROM users WHERE phone='$PHONE'")
}

# login PHONE -> TOKEN REFRESH : a new session for an account that exists
login() {
  local body code v
  next_ip
  body=$(curl -s --interface "$IPADDR" -X POST "$API/auth/otp/request" -H 'Content-Type: application/json' -d "{\"phone\":\"$1\"}")
  code=$(printf '%s' "$body" | j '.dev_code // empty')
  v=$(curl -s -X POST "$API/auth/otp/verify" -H 'Content-Type: application/json' -d "{\"phone\":\"$1\",\"code\":\"$code\"}")
  TOKEN=$(printf '%s' "$v" | j '.access_token // empty'); REFRESH=$(printf '%s' "$v" | j '.refresh_token // empty')
}
b64d() { local s=$1; while [ $(( ${#s} % 4 )) -ne 0 ]; do s="$s="; done; printf '%s' "$s" | tr '_-' '/+' | base64 -d 2>/dev/null; }
b64e() { base64 -w0 | tr '+/' '-_' | tr -d =; }

# utc "YYYY-MM-DD HH:MM" (Athens wall clock) -> RFC3339 UTC
utc() { date -u -d "@$(TZ=Europe/Athens date -d "$1" +%s)" +%FT%TZ; }
iso_in() { date -u -d "$1" +%FT%TZ; }

# parallel N METHOD PATH TOKEN [BODY] -> PCODES (summary), files $TMP/par.I and $TMP/parc.I
parallel() {
  local n=$1 m=$2 p=$3 t=$4 b=${5-} i
  local args=(-s -m 30 -X "$m" "$API$p" -H 'Content-Type: application/json')
  [ -n "$t" ] && args+=(-H "Authorization: Bearer $t")
  [ -n "$b" ] && args+=(-d "$b")
  local pids=()
  for i in $(seq 1 "$n"); do
    ( curl "${args[@]}" -o "$TMP/par.$i" -w '%{http_code}' > "$TMP/parc.$i" 2>/dev/null ) &
    pids+=($!)
  done
  wait "${pids[@]}"   # not a bare wait: the chat-socket clients are children too
  PCODES=$(for i in $(seq 1 "$n"); do cat "$TMP/parc.$i"; echo; done | sort | uniq -c | awk '{printf "%sx%s ", $1, $2}')
}

# request bodies --------------------------------------------------------
# rbody ORIGIN_LON ORIGIN_LAT DEST_LON DEST_LAT WINDOW_START WINDOW_END [extra json members]
rbody() {
  printf '{"origin_lon":%s,"origin_lat":%s,"destination_lon":%s,"destination_lat":%s,"origin_label":"Αφετηρία","destination_label":"Προορισμός","window_start":"%s","window_end":"%s"%s}' \
    "$1" "$2" "$3" "$4" "$5" "$6" "${7:+,$7}"
}
route_body() { # date earliest latest [origin_lon origin_lat dest_lon dest_lat] [origin_label]
  local ol=${4:-22.9444} oa=${5:-40.6401} dl=${6:-23.5480} da=${7:-41.0855} lab=${8:-Θεσσαλονίκη}
  printf '{"origin_lon":%s,"origin_lat":%s,"destination_lon":%s,"destination_lat":%s,"origin_label":"%s","destination_label":"Σέρρες","seats":3,"kind":"oneoff","oneoff_date":"%s","depart_earliest":"%s","depart_latest":"%s"}' \
    "$ol" "$oa" "$dl" "$da" "$lab" "$1" "$2" "$3"
}
car() { req POST /me/vehicles "$1" "{\"make\":\"Toyota\",\"model\":\"Yaris\",\"fuel\":\"petrol\",\"engine_cc\":1330,\"colour\":\"ασημί\",\"plate\":\"$2\",\"label\":\"το αμάξι\"}"; CARID=$(printf '%s' "$RES" | j .id); }

# ------------------------------------------------------------------ setup ---
section "setup: nine accounts, two drivers, a trip leaving in ${LEAD_MIN} minutes"
TODAY=$(TZ=Europe/Athens date +%F)
NOWS=$(date +%s)
E1=$(( (NOWS / 60 + LEAD_MIN) * 60 )); L1=$((E1 + 60))
EH=$(TZ=Europe/Athens date -d "@$E1" +%H:%M); LH=$(TZ=Europe/Athens date -d "@$L1" +%H:%M)
if [[ $EH > "23:20" || $EH < "00:10" ]]; then echo "too close to midnight for a same-day trip ($EH); rerun after 00:10" >&2; exit 2; fi
RUNDAY=$(( ( $(date +%s) / 3600 ) % 17 + 2 ))   # a different quiet day per hour, 2..18 days ahead
QUIET=$(TZ=Europe/Athens date -d "+$RUNDAY days" +%F)
TOMORROW=$(TZ=Europe/Athens date -d tomorrow +%F)
PLATE=$(printf 'QA%05d' $((RANDOM * 3 % 100000)))

signup "Οδηγός Δ";   D=$TOKEN;  DU=$USER_ID;  DPHONE=$PHONE
signup "Οδηγός Ε";   E=$TOKEN;  EU=$USER_ID;  EPHONE=$PHONE
signup "Επιβάτης Α"; A=$TOKEN;  AU=$USER_ID;  APHONE=$PHONE
signup "Επιβάτης Β"; B=$TOKEN;  BU=$USER_ID;  BPHONE=$PHONE
signup "Επιβάτης Γ"; P3=$TOKEN; P3U=$USER_ID
signup "Επιβάτης Ζ"; C=$TOKEN;  CU=$USER_ID
signup "Επιβάτης Η"; C2=$TOKEN; C2U=$USER_ID
signup "Ξένος Σ";    S=$TOKEN;  SU=$USER_ID;  SPHONE=$PHONE
signup "Διαγραφή Θ"; Z=$TOKEN;  ZU=$USER_ID;  ZREFRESH=$REFRESH
for v in D E A B P3 C C2 S Z; do [ -n "${!v}" ] || { echo "setup failed: no token for $v" >&2; exit 2; }; done
ok "nine accounts signed up"

car "$D" "${PLATE}D"; DVEH=$CARID; car "$E" "${PLATE}E"; EVEH=$CARID
# D: Θεσσαλονίκη -> Σέρρες, leaving in a few minutes. The origin label is a
# street, which is what drivers type; see "labels" below for who may see it.
req POST /routes "$D" "$(route_body "$TODAY" "$EH" "$LH" 22.9444 40.6401 23.5480 41.0855 'Οδός Μυστική 12 Θεσσαλονίκη')"
OCC=$(printf '%s' "$RES" | j .first_occurrence_id); TPL=$(printf '%s' "$RES" | j .template_id)
want "D publishes the trip that leaves at $EH" 201
# E: the opposite way at the same time, so its asks cannot land on D
req POST /routes "$E" "$(route_body "$TODAY" "$EH" "$LH" 23.5480 41.0855 22.9444 40.6401 Σέρρες)"
OCCE=$(printf '%s' "$RES" | j .first_occurrence_id); TPLE=$(printf '%s' "$RES" | j .template_id)
want "E publishes the opposite trip" 201
# D again, tomorrow, with markup in the label, for the shared-route page
req POST /routes "$D" "$(route_body "$TOMORROW" 08:00 08:30 22.9444 40.6401 23.5480 41.0855 '<i>x</i> & Θεσσαλονίκη')"
TPLH=$(printf '%s' "$RES" | j .template_id)
note "D route with markup in its label: [$CODE] $(excerpt 100)"

W1=$(iso_in '+1 minute'); W2=$(iso_in '+2 hours')
mk() { # token name olon olat dlon dlat -> sets REQ, ASK
  req POST /requests "$1" "$(rbody "$3" "$4" "$5" "$6" "$W1" "$W2")"
  REQ=$(printf '%s' "$RES" | j .request_id); ASK=$(printf '%s' "$RES" | j .ask_id)
  [ -n "$ASK" ] && [ "$ASK" != null ] && ok "$2 asked [$CODE]" || bad "$2 got an ask" "$LAST -> $CODE $(excerpt)"
}
# a few metres apart: the matcher only asks about a second party when the
# itinerary with both stays inside the driver's detour limits
mk "$A"  A   22.95   40.65   23.54   41.08;    RA=$REQ;  AA=$ASK
mk "$B"  B   22.9501 40.6501 23.5401 41.0801;  RB=$REQ;  AB=$ASK
mk "$P3" P3  22.9502 40.6502 23.5402 41.0802;  RP3=$REQ; AP3=$ASK
mk "$C"  C   23.54   41.08   22.95   40.65;    RC=$REQ;  AC=$ASK
mk "$C2" C2  23.5401 41.0801 22.9501 40.6501;  RC2=$REQ; AC2=$ASK
mk "$S"  S   23.5402 41.0802 22.9502 40.6502;  RS=$REQ;  AS=$ASK
dbeq "D holds three pending asks (three parties, a 3-seat car)" "SELECT count(*) FROM asks WHERE occurrence_id='$OCC' AND status='pending'" 3

# ============================================================ 1. IDOR =======
section "1. IDOR on asks"
req GET "/asks/$AA" "$S";            want "S reads D's ask" '404'
req GET "/asks/$AA" "$E";            want "E (another driver) reads D's ask" '404'
req GET "/asks/$AA" "$B";            want "B reads A's ask" '404'
req GET "/asks/$AA" "$D";            want "D reads own ask (control)" '200'
lacks "the driver's ask view has no phone or coordinates" '"phone"'
req POST "/asks/$AA/accept" "$E";    want "E accepts D's ask" '403|404'
req POST "/asks/$AA/accept" "$A";    want "A (the passenger) accepts her own ask" '403|404'
req POST "/asks/$AA/accept" "$S";    want "S accepts D's ask" '403|404'
req POST "/asks/$AA/decline" "$S";   want "S declines D's ask" '403|404'
req POST "/asks/$AA/decline" "$A";   want "A declines (cancels?) the ask meant for D" '403|404'
req POST "/asks/$AA/decline" "$B";   want "B declines A's ask" '403|404'
dbeq "A's ask is still pending after all that" "SELECT status FROM asks WHERE id='$AA'" pending
req GET /asks/pending "$E";          lacks "E's pending list does not carry D's asks" "$AA"
req GET /asks/pending "$S";          lacks "S's pending list does not carry D's asks" "$AA"
# 403 vs 404: an existing ask answers differently from a missing one
req POST "/asks/$AA/accept" "$E"; c1=$CODE
req POST "/asks/00000000-0000-4000-8000-000000000000/accept" "$E"; c2=$CODE
[ "$c1" = "$c2" ] && ok "accepting somebody else's ask looks like accepting a missing one [$c1]" \
  || bad "ask id existence oracle" "POST /asks/<D's ask>/accept as E -> $c1, missing id -> $c2 $(excerpt 90) (error.code not_yours vs ask_closed/not_found)"

section "1b. IDOR on requests, alternatives, bookings, trips, routes"
req GET "/requests/$RA/alternatives" "$S"; want "S reads alternatives of A's request" '404'
req GET "/requests/$RA/alternatives" "$B"; want "B reads alternatives of A's request" '404'
req GET "/requests/$RA/alternatives" "$D"; want "D reads alternatives of A's request" '404'
req GET "/requests/$RA/alternatives" "$A"; want "A reads own alternatives (control)" '200'
req GET /requests/mine "$B";               lacks "B's request list does not carry A's request" "$RA"
req DELETE "/requests/$RA" "$S";           want "S cancels A's request" '404'
req DELETE "/requests/$RA" "$B";           want "B cancels A's request" '404'
req DELETE "/requests/$RA" "$D";           want "D cancels A's request" '404'
dbeq "A's request is untouched" "SELECT status FROM ride_requests WHERE id='$RA'" asking
req GET "/requests/$RA" "$A";              want "no GET /requests/{id} exists (404/405 is fine)" '404|405'
# a stranger's token on routes it does not own
req PUT "/me/routes/$TPL" "$E" "$(route_body "$TODAY" "$EH" "$LH" 22.9444 40.6401 23.5480 41.0855 Θεσσαλονίκη)"
want "E edits D's route" '403|404'
req DELETE "/me/routes/$TPL" "$E";         want "E retires D's route" '403|404'
req POST "/me/routes/$TPL/share" "$E";     want "E creates a share link for D's route" '403|404'
req DELETE "/me/routes/$TPL/share" "$E";   want "E revokes D's share link" '403|404'
req PUT "/me/routes/$TPL/nudge" "$E" '{"on":true}'; want "E turns on D's nudge" '403|404'
req PUT "/me/routes/$TPL" "$A" "$(route_body "$TODAY" "$EH" "$LH")"; want "A (not a driver) edits D's route" '403|404|409'
dbeq "D's route is still live" "SELECT count(*) FROM route_templates WHERE id='$TPL' AND active" 1
req DELETE "/trips/$OCC" "$A";             want "A cancels D's occurrence" '404'
req DELETE "/trips/$OCC" "$S";             want "S cancels D's occurrence" '404'
req DELETE "/trips/$OCC" "$E";             want "E cancels D's occurrence" '404'
dbeq "D's occurrence is not cancelled" "SELECT status FROM trip_occurrences WHERE id='$OCC'" scheduled
req GET /me/routes "$E";                   lacks "E's route list does not show D's template" "$TPL"
NOWZ=$(iso_in 'now')
for who in A S E; do
  req POST "/trips/$OCC/drive" "${!who}" "{\"departs_at\":\"$NOWZ\"}"; want "$who starts D's drive" '403|404'
  req PATCH "/trips/$OCC/drive" "${!who}" '{"offering":false}';           want "$who stops D's drive taking people" '403|404'
  req POST "/trips/$OCC/drive/end" "${!who}";                             want "$who ends D's drive" '403|404'
done
req POST "/trips/$OCC/here" "$D"; want "D (the driver) taps «Είμαι εδώ», a passenger action" '403|404|409'
req POST "/trips/$OCC/here" "$S"; want "S taps «Είμαι εδώ» on D's trip" '403|404'
dbeq "D's drive was not started by anybody else" "SELECT drive_set_at IS NULL AND drive_ended_at IS NULL FROM trip_occurrences WHERE id='$OCC'" t
# somebody else's car and group
req DELETE "/me/vehicles/$DVEH" "$E";            want "E retires D's car" '403|404'
req POST "/me/vehicles/$DVEH/default" "$E";      want "E makes D's car her default" '403|404'
req POST /routes "$E" "$(route_body "$TOMORROW" 09:00 09:30 | jq -c --arg v "$DVEH" '.vehicle_id=$v')"
want "E publishes a route in D's car (vehicle_id of somebody else)" '400|403|404|409'
req GET /me/vehicles "$E";                       lacks "E's garage does not list D's plate" "${PLATE}D"
dbeq "D's car is still on the road" "SELECT count(*) FROM vehicles WHERE id='$DVEH' AND retired_at IS NULL" 1
# moderation endpoints for a normal user
req GET /moderation/reports "$A";          want "A reads the moderation queue" '403|404'
req POST "/moderation/users/$DU/reinstate" "$A"; want "A reinstates a user" '403|404'
req POST /moderation/meeting-points/00000000-0000-4000-8000-000000000000/approve "$A"; want "A approves a meeting point" '403|404'
# groups
req POST /groups "$D" '{"name":"Συνεργάτες"}'; GID=$(printf '%s' "$RES" | j .id); want "D creates a group" '200|201'
req POST /routes "$E" "$(route_body "$TOMORROW" 09:00 09:30 | jq -c --arg g "$GID" '.group_id=$g')"
want "E publishes a route into a group she is not in" '400|403|404|409'
req POST "/groups/$GID/code" "$S";         want "S rotates D's group code" '403|404'
req GET "/groups/$GID/stats" "$S";         want "S reads D's group stats" '403|404'
req GET "/groups/$GID/trips.csv?month=${TODAY:0:7}" "$S";     want "S exports D's group trips" '403|404'
req DELETE "/groups/$GID/membership" "$S"; note "S leaves a group it is not in: [$CODE]"

# ======================================================= 7a. accept races ===
section "6. concurrency: five accepts of one ask, three asks on two parties"
parallel 5 POST "/asks/$AA/accept" "$D"
note "5 parallel accepts of A's ask: $PCODES"
dbeq "exactly one booking for the ask" "SELECT count(*) FROM bookings WHERE ask_id='$AA'" 1
BA=$(q "SELECT id FROM bookings WHERE ask_id='$AA'")
dbeq "seats_free fell by one, not five" "SELECT seats_free FROM trip_occurrences WHERE id='$OCC'" 2
n200=$(for i in 1 2 3 4 5; do cat "$TMP/parc.$i"; echo; done | grep -c '^200$')
[ "$n200" = 1 ] && ok "one 200 among the five" || bad "parallel accept of the same ask" "$n200 of 5 answered 200 ($PCODES)"
BBODY=$(rbody 22.9501 40.6501 23.5401 41.0801 "$W1" "$W2")
req POST "/asks/$AB/accept" "$D"
if [ "$CODE" = 200 ]; then
  ok "D accepts B's ask, pending beside A's"; BB=$(printf '%s' "$RES" | j .booking_id)
else
  bad "a second party's pending ask stays acceptable after the first was accepted" \
    "POST /asks/<B's ask>/accept -> $CODE $(excerpt 130); the ask is still '$(q "SELECT status FROM asks WHERE id='$AB'")' and B's request '$(q "SELECT status FROM ride_requests WHERE id='$RB'")' (itinerary_version moved on A's accept)"
  req POST /requests "$B" "$BBODY"
  [ "$(printf '%s' "$RES" | j .ask_id)" = "$AB" ] && note "B re-posting the same journey is handed the same stale ask" || note "B re-posting: [$CODE] $(excerpt 120)"
  req DELETE "/requests/$RB" "$B"
  req POST /requests "$B" "$BBODY"; RB=$(printf '%s' "$RES" | j .request_id); AB=$(printf '%s' "$RES" | j .ask_id)
  req POST "/asks/$AB/accept" "$D"; BB=$(printf '%s' "$RES" | j .booking_id); want "after cancel + re-post, D accepts B" 200
fi
req POST "/asks/$AP3/accept" "$D"
want "P3's ask, a third party past the cap of two, is a clean 409 and not a 5xx" '409'
req DELETE "/requests/$RP3" "$P3"
req POST /requests "$P3" "$(rbody 22.9502 40.6502 23.5402 41.0802 "$W1" "$W2")"
[ "$(printf '%s' "$RES" | j .asking)" = false ] && ok "a third party is not asked while two are aboard" || bad "a third party was asked about a full itinerary" "$LAST -> $CODE $(excerpt)"
RP3=$(printf '%s' "$RES" | j .request_id)
req GET "/requests/$RP3/alternatives" "$P3"
lacks "alternatives shown to a passenger carry no other driver's free-text street label" 'Μυστική'
note "alternatives for P3: $(excerpt 220)"
req POST /requests "$P3" "$(rbody 22.9502 40.6502 23.5402 41.0802 "$W1" "$W2" '"seats":4')"
if [ "$(printf '%s' "$RES" | j .asking)" = false ]; then ok "4 seats against a 3-seat car (and 1 free) asks nobody [$CODE]"; else bad "a party of four was put to a driver with one seat free" "$LAST -> $CODE $(excerpt)"; fi
req DELETE "/requests/$(printf '%s' "$RES" | j .request_id)" "$P3"
dbeq "two confirmed bookings, no third" "SELECT count(*) FROM bookings WHERE occurrence_id='$OCC' AND status='confirmed'" 2
req POST "/asks/$AA/accept" "$D";          want "accepting an accepted ask again" '409|200'
note "second accept of an accepted ask: [$CODE] $(excerpt 120)"

section "ask states: declined (E, S's ask)"
req POST "/asks/$AS/decline" "$S";   want "S (the passenger) declines her own ask" '403|404'
req POST "/asks/$AS/decline" "$E";   want "E declines S's ask" 204
req POST "/asks/$AS/accept" "$E";    want "E accepts a declined ask" 409
has "... as ask_closed" ask_closed
req POST "/asks/$AS/decline" "$E";   note "declining it again: [$CODE] $(excerpt 100)"
req GET /requests/mine "$S"; note "S's request after the decline: $(printf '%s' "$RES" | jq -c '[.requests[]|{status,asking_now,drivers_declined}]' 2>/dev/null)"

section "6b. concurrency: C cancels her request while E accepts her ask"
parallel 1 POST "/asks/$AC/accept" "$E" &
PIDW=$!
req DELETE "/requests/$RC" "$C"; CANCELCODE=$CODE
wait $PIDW 2>/dev/null
ACC=$(cat "$TMP/parc.1"); note "cancel -> $CANCELCODE, accept -> $ACC"
RSTAT=$(q "SELECT status FROM ride_requests WHERE id='$RC'"); BKC=$(q "SELECT count(*) FROM bookings WHERE request_id='$RC' AND status='confirmed'")
if { [ "$RSTAT" = matched ] && [ "$BKC" = 1 ]; } || { [ "$RSTAT" = cancelled ] && [ "$BKC" = 0 ]; }; then
  ok "request and booking agree after the race (request $RSTAT, bookings $BKC)"
else bad "cancel/accept race left an inconsistent state" "request=$RSTAT confirmed bookings=$BKC cancel=$CANCELCODE accept=$ACC"; fi
if [ "$CANCELCODE" = 204 ] && [ "$ACC" = 200 ]; then bad "cancel and accept both reported success" "cancel 204 and accept 200 for the same request"; fi

section "6c. ten identical POST /requests at once -> one request"
PAR=$P3; PARU=$P3U
PW1=$(utc "$QUIET 07:00"); PW2=$(utc "$QUIET 10:00")
parallel 10 POST /requests "$PAR" "$(rbody 22.95 40.65 23.54 41.08 "$PW1" "$PW2")"
note "10 parallel posts: $PCODES"
dbeq "one open request for the party" "SELECT count(*) FROM ride_requests WHERE passenger_id='$PARU' AND status IN ('open','asking') AND window_start >= '$PW1'" 1
ids=$(for i in $(seq 1 10); do jq -r .request_id < "$TMP/par.$i" 2>/dev/null; done | sort -u | wc -l)
[ "$ids" = 1 ] && ok "all ten answers name the same request" || bad "ten posts, $ids different request ids" "$PCODES"

# ============================================ 2. data exposure between users ==
section "2. what B learns about A, and what a stranger learns"
req GET "/trips/$OCC" "$B"
want "B reads the trip" 200
lacks "B's trip view carries no phone number (A)" "$APHONE"
lacks "B's trip view carries no phone number (D)" "$DPHONE"
lacks "B's trip view carries no phone field" '"phone"'
lacks "B does not get A's pickup" '[22.95,40.65]'
lacks "B does not get A's drop-off" '[23.54,41.08]'
has   "B gets his own pickup" '[22.9501,40.6501]'
lacks "B gets no date of birth" 'date_of_birth'
lacks "B gets no email-like or home-region field for A" 'home_region_id'
lacks "B gets no booking id of A (only the driver may)" "$BA"
PRICE_A=$(printf '%s' "$RES" | jq -r --arg a "$AU" '.passengers[]|select(.id==$a)|.price_eur')
[ -z "$PRICE_A" ] || [ "$PRICE_A" = null ] && ok "B does not see what A pays" \
  || bad "co-passenger sees the price A pays" "GET /trips/$OCC as B -> passengers[A].price_eur=$PRICE_A (also seats, bio, rating)"
req GET "/trips/$OCC" "$D"
has   "D (control) sees A's pickup" '[22.95,40.65]'
lacks "D's view of the trip carries no passenger phone" "$APHONE"
req GET "/trips/$OCC" "$S";                want "S reads the trip" '404'
req GET "/trips/$OCC/messages" "$S";       want "S reads the chat" '404'
req POST "/trips/$OCC/messages" "$S" '{"body":"hola"}'; want "S posts to the chat" '404'
req GET "/trips/$OCC/live" "$S";           want "S reads live positions" '404'
req GET "/trips/$OCC" "$E";                want "E (other driver) reads the trip" '404'
req GET "/trips/$OCC" "$P3";               want "P3 (ask refused by the cap) reads the trip" '404'
req GET "/trips/00000000-0000-4000-8000-000000000000" "$S"; c0=$CODE
[ "$c0" = 404 ] && ok "S sees the same 404 for a missing trip" || bad "trip existence oracle" "missing trip -> $c0"
req GET /trips/upcoming "$S";              lacks "S's upcoming trips carry nothing of D's" "$OCC"
req GET /trips/past "$S";                  want "S past trips" 200
req GET "/users/$DU/photo" "$S";           note "S fetches D's photo (none uploaded): [$CODE]"
req GET "/users/$DU" "$S";                 want "there is no public profile document" '404|405'
req GET "/users/$DU/profile" "$S";         want "there is no public profile document (2)" '404|405'
req GET /live/map "$S"
want "S reads the live map" 200
lacks "live map has no driver id" "$DU"
lacks "live map has no phone" "$DPHONE"
lacks "live map has no street label" "Μυστική"
req GET /demand "$S";                      lacks "demand counts carry no user id" "$AU"
req POST /supply "$S" "{\"origin_lon\":22.9444,\"origin_lat\":40.6401,\"destination_lon\":23.548,\"destination_lat\":41.0855,\"date\":\"$TODAY\"}"
want "S asks supply" 200
lacks "supply answers a word, never a number or driver" "$DU"
has   "supply answer is several|unknown" '"status"'
# the export is the caller's data
req GET /me/export "$B"
want "B exports his data" 200
lacks "B's export carries no phone of A" "$APHONE"
lacks "B's export carries no phone of D" "$DPHONE"
lacks "B's export carries no pickup of A" '22.95'
# a price/receipt of another seat
req GET "/trips/$OCC" "$B"; lacks "B's trip has no price_breakdown of A's seat (A's fuel receipt)" '"price_breakdown":{"v":2,"basis_v":3,"consumption_l_100km":5.5,"consumption_source":"band","fuel":"petrol","fuel_price_eur":1.859,"region_name":"Μητροπολιτική Ενότητα Θεσσαλονίκης","price_is_regional":false,"price_source":"configured at deploy","trip_km":0'
note "(B's own price_breakdown is expected; the check above only trips if A's is echoed to B)"
# the ask view the driver gets: no passenger coordinates, no phone
req GET /asks/pending "$D"; lacks "driver's ask list: no phone" '"phone"'
lacks "driver's ask list: no coordinates" '"origin_lon"'

section "2b. chat: shared by every passenger on the trip"
req POST "/trips/$OCC/messages" "$A" '{"body":"Είμαι η Α, το τηλέφωνό μου είναι 6900000000"}'
want "A posts to the trip chat" 201
req GET "/trips/$OCC/messages" "$B"
has  "B reads A's chat line to the driver (chat is one thread for the whole car)" 'Είμαι η Α'
note "design note: the thread is shared by all passengers, a phone number typed by A reaches B and no contact scan applies to chat"

section "3. live location"
req PUT "/trips/$OCC/position" "$A" '{"lat":40.65,"lon":22.95,"accuracy_m":10}';  want "A uploads a position on her own trip" 204
req PUT "/trips/$OCC/position" "$B" '{"lat":40.66,"lon":22.96,"accuracy_m":10}';  want "B uploads a position on his trip" 204
req PUT "/trips/$OCC/position" "$D" '{"lat":40.64,"lon":22.94,"accuracy_m":10}';  want "D uploads a position" 204
req PUT "/trips/$OCC/position" "$S" '{"lat":40.65,"lon":22.95}';  want "S uploads a position on a trip it is not on" '404'
req PUT "/trips/$OCC/position" "$P3" '{"lat":40.65,"lon":22.95}'; want "P3 (refused by the cap) uploads on the trip" '404'
req PUT "/trips/$OCCE/position" "$A" '{"lat":40.65,"lon":22.95}'; want "A uploads on E's trip" '404'
req PUT "/trips/$OCC/position" "$A" '{"lat":91,"lon":22.95}';     want "latitude 91" 400
req PUT "/trips/$OCC/position" "$A" '{"lat":"NaN","lon":22.95}';  want "latitude \"NaN\"" 400
req PUT "/trips/$OCC/position" "$A" '{"lon":22.95}';              want "missing latitude" 400
req PUT "/trips/$OCC/position" "$A" '{"lat":40.65,"lon":22.95,"accuracy_m":-5}'; want "negative accuracy" 400
sleep 6
req PUT "/trips/$OCC/position" "$A" "{\"lat\":40.651,\"lon\":22.951,\"accuracy_m\":10,\"observed_at\":\"$(iso_in '+1 day')\"}"
want "a fix claiming to be from tomorrow" '204|400'
FUT=$(q "SELECT count(*) FROM trip_live_positions WHERE occurrence_id='$OCC' AND user_id='$AU' AND observed_at > now() + interval '10 seconds'")
[ "$FUT" = 0 ] && ok "a future observed_at is not stored as such" || bad "future observed_at stored" "observed_at > now()+10s for A's row"
sleep 6
req PUT "/trips/$OCC/position" "$A" "{\"lat\":40.65,\"lon\":22.95,\"accuracy_m\":10,\"observed_at\":\"2020-01-01T00:00:00Z\"}"
want "a fix from 2020" '400'
req PUT "/trips/$OCC/position" "$A" "{\"lat\":40.65,\"lon\":22.95,\"observed_at\":\"$(iso_in '-5 minutes')\"}"
want "a fix five minutes old" '400'
req GET "/trips/$OCC/live" "$A"
want "A reads the live view" 200
lacks "A does not see B's position" '40.66'
lacks "A does not see B's id" "$BU"
has   "A sees the driver's" "$DU"
req GET "/trips/$OCC/live" "$B"
lacks "B does not see A's position" '40.65'
lacks "B does not see A's id" "$AU"
req GET "/trips/$OCC/live" "$D"
has   "D sees A" "$AU"; has "D sees B" "$BU"
note "$(jq -c '[.positions[]|{name,lat,lon}]' <<<"$RES")"
# unthrottled?
t0=$(date +%s%N); okc=0
for i in $(seq 1 15); do req PUT "/trips/$OCC/position" "$A" '{"lat":40.65,"lon":22.95,"accuracy_m":10}'; [ "$CODE" = 204 ] && okc=$((okc+1)); done
note "15 uploads in a row: $okc accepted, $(( ($(date +%s%N) - t0) / 1000000 )) ms, no 429 (the write is throttled to one per 5 s inside the service, the HTTP call is not)"

section "4. B cancels: seat returned once, position gone"
# a chat socket opened before the cancel: is it closed, or does it go on listening?
cat > "$TMP/ws.mjs" <<'JS'
const [url, token, out, ms] = process.argv.slice(2);
import('node:fs').then(({ appendFileSync, writeFileSync }) => {
  writeFileSync(out, '');
  let ws;
  try { ws = new WebSocket(url, { headers: { Authorization: 'Bearer ' + token } }); }
  catch (e) { appendFileSync(out, 'CONSTRUCT-ERROR ' + e + '\n'); process.exit(0); }
  ws.onopen = () => appendFileSync(out, 'OPEN\n');
  ws.onmessage = (m) => appendFileSync(out, 'FRAME ' + m.data + '\n');
  ws.onclose = (e) => { appendFileSync(out, 'CLOSE ' + e.code + '\n'); process.exit(0); };
  ws.onerror = () => appendFileSync(out, 'ERROR\n');
  setTimeout(() => process.exit(0), Number(ms));
});
JS
WSURL=${API/http/ws}/trips/$OCC/socket
node "$TMP/ws.mjs" "$WSURL" "$B" "$TMP/ws.b" 90000 &
WSB=$!
node "$TMP/ws.mjs" "$WSURL" "$S" "$TMP/ws.s" 90000 &
WSS=$!
sleep 2
req POST "/trips/$OCC/messages" "$A" '{"body":"before-B-left-MARK"}'
sleep 1
SF0=$(q "SELECT seats_free FROM trip_occurrences WHERE id='$OCC'")
req DELETE "/bookings/$BB" "$S";  want "S cancels B's booking" '403|404'
req DELETE "/bookings/$BB" "$A";  want "A cancels B's booking" '403|404'
req DELETE "/bookings/$BB" "$E";  want "E cancels B's booking" '403|404'
req DELETE "/bookings/$BB" "$P3"; want "P3 cancels B's booking" '403|404'
dbeq "B's booking is still confirmed" "SELECT status FROM bookings WHERE id='$BB'" confirmed
req DELETE "/bookings/$BB" "$B";  want "B cancels his booking" 204
req DELETE "/bookings/$BB" "$B";  want "B cancels it a second time" '204|409'
dbeq "the seat came back exactly once" "SELECT seats_free FROM trip_occurrences WHERE id='$OCC'" $((SF0 + 1))
parallel 5 DELETE "/bookings/$BB" "$B"; note "5 parallel cancels of a cancelled booking: $PCODES"
dbeq "still exactly once after the parallel repeats" "SELECT seats_free FROM trip_occurrences WHERE id='$OCC'" $((SF0 + 1))
req DELETE "/bookings/$BB" "$S"; c1=$CODE
req DELETE "/bookings/00000000-0000-4000-8000-000000000000" "$S"; c2=$CODE
[ "$c1" = "$c2" ] && ok "cancelling somebody else's booking looks like cancelling a missing one [$c1]" \
  || bad "booking id existence oracle" "DELETE /bookings/<B's booking> as S -> $c1, missing id -> $c2 (not_yours vs not_found)"
req POST "/trips/$OCC/messages" "$A" '{"body":"after-B-left-SECRET"}'
want "A posts to the chat after B left" 201
sleep 2
note "B's socket log: $(tr '\n' ' ' < "$TMP/ws.b" | head -c 400)"
if ! grep -q 'before-B-left-MARK' "$TMP/ws.b" 2>/dev/null; then note "B's chat socket never delivered the control message (needs node >= 22 for a global WebSocket): $(head -c 120 "$TMP/ws.b" 2>/dev/null)"
elif grep -q 'after-B-left-SECRET' "$TMP/ws.b"; then
  bad "a chat socket outlives the seat" "B opened GET /v1/trips/$OCC/socket, then cancelled his booking; a message posted afterwards by A was still pushed to B's socket (authorization is checked at connect only: ws_handler.go handleTripSocket)"
else ok "B's chat socket does not receive messages posted after he cancelled"; fi
grep -q '^OPEN' "$TMP/ws.s" 2>/dev/null && bad "a stranger can open the chat socket" "S got an upgrade on /socket" || ok "S cannot open the chat socket"
kill $WSB $WSS 2>/dev/null
req PUT "/trips/$OCC/position" "$B" '{"lat":40.66,"lon":22.96}'; want "B uploads after cancelling" '404'
req GET "/trips/$OCC/live" "$B";   want "B reads live after cancelling" '404'
req GET "/trips/$OCC" "$B";        want "B reads the trip after cancelling" '404'
req GET "/trips/$OCC/messages" "$B"; want "B reads the chat after cancelling" '404'
req POST "/trips/$OCC/messages" "$B" '{"body":"still here"}'; want "B posts to the chat after cancelling" '404'
req GET "/trips/$OCC/live" "$D"; lacks "D no longer sees B" "$BU"
dbeq "B's last position is erased when the seat is cancelled" \
  "SELECT count(*) FROM trip_live_positions WHERE occurrence_id='$OCC' AND user_id='$BU'" 0

section "4b. standing arrangements (repeat a booking) and again"
req POST "/bookings/$BA/repeat" "$B";  want "B repeats A's booking" '403|404'
req POST "/bookings/$BA/repeat" "$S";  want "S repeats A's booking" '403|404'
req POST "/bookings/$BA/again" "$S";   want "S asks again on A's booking" '403|404'
req POST "/bookings/$BA/again" "$B";   want "B asks again on A's booking" '403|404'
req POST "/bookings/$BA/repeat" "$A";  want "A repeats her booking" 200
AR=$(printf '%s' "$RES" | j .arrangement_id)
if [ -n "$AR" ] && [ "$AR" != null ]; then
  for who in S B E; do
    req POST "/arrangements/$AR/approve" "${!who}"; want "$who approves A's arrangement" '403|404'
    req POST "/arrangements/$AR/decline" "${!who}"; want "$who declines A's arrangement" '403|404'
    req DELETE "/arrangements/$AR" "${!who}";       want "$who cancels A's arrangement" '403|404'
  done
  req POST "/arrangements/$AR/approve" "$A"; want "A approves her own arrangement (only the driver may)" '403|404'
  req GET /arrangements "$S"; lacks "S's arrangement list does not carry A's" "$AR"
  req GET /arrangements "$B"; lacks "B's arrangement list does not carry A's" "$AR"
  req GET /arrangements "$D"; has "D's arrangement list carries it (control)" "$AR"
  lacks "D's arrangement list carries no phone" "$APHONE"
  dbeq "A's arrangement is still pending" "SELECT status FROM standing_arrangements WHERE id='$AR'" pending
fi
req POST "/users/00000000-0000-4000-8000-000000000000/block" "$S"
lacks "blocking a missing user does not quote the database error" 'store:'
note "block a missing user: [$CODE] $(excerpt 160)"
req POST "/users/$SU/block" "$S";      note "S blocks herself: [$CODE] $(excerpt 120)"
req POST "/users/00000000-0000-4000-8000-000000000000/report" "$S" '{"reason":"x"}'; note "report a missing user: [$CODE] $(excerpt 120)"
req POST "/users/$DU/report" "$S" "{\"reason\":\"$(head -c 30000 /dev/zero | tr '\0' 'r')\"}"
if [ "$CODE" = 204 ]; then bad "report reason is unbounded" "POST /users/$DU/report reason=30000 chars -> 204 (reports.reason: $(q "SELECT max(length(reason)) FROM reports") chars max stored)"; else ok "oversized report reason refused [$CODE]"; fi

# ===================================== 4b. state machine on requests/labels ===
section "4c. request validation"
SW1=$(utc "$QUIET 07:00"); SW2=$(utc "$QUIET 10:00")
for s in 0 -1 5 100 1.5 '"2"' null; do
  req POST /requests "$S" "$(rbody 22.95 40.65 23.54 41.08 "$SW1" "$SW2" "\"seats\":$s")"
  if [ "$s" = null ]; then want "seats null (= default)" '200|201'; else want "seats $s" 400; fi
  # null made a real request for S on the quiet day; cancel it again
  [ "$s" = null ] && req DELETE "/requests/$(printf '%s' "$RES" | j .request_id)" "$S" ""
done
req POST /requests "$S" "$(rbody 22.95 40.65 23.54 41.08 "$SW2" "$SW1")";  want "window_end before window_start" 400
req POST /requests "$S" "$(rbody 22.95 40.65 23.54 41.08 "$SW1" "$SW1")";  want "window_end equal to window_start" 400
req POST /requests "$S" "$(rbody 22.95 40.65 23.54 41.08 "$(iso_in '-3 hours')" "$(iso_in '-2 hours')")"; want "window in the past" 400
req POST /requests "$S" "$(rbody 22.95 40.65 23.54 41.08 "$SW1" "$(iso_in '+400 days')")"; want "window a year out" 400
req POST /requests "$S" "$(rbody 22.95 40.65 22.95 40.65 "$SW1" "$SW2")";  want "origin equals destination" 400
req POST /requests "$S" "$(rbody 40.65 22.95 41.08 23.54 "$SW1" "$SW2")";  want "latitude and longitude swapped" 400
req POST /requests "$S" "$(rbody 2.35 48.85 2.45 48.9 "$SW1" "$SW2")";      want "both ends in Paris" 400
req POST /requests "$S" "$(rbody 1e308 1e308 -1e308 -1e308 "$SW1" "$SW2")"; want "coordinates at 1e308" 400
req POST /requests "$S" "$(rbody 22.95 40.65 23.54 41.08 "$SW1" "$SW2" '"luggage":"piano"')"; want "unknown luggage size" 400
req POST /requests "$S" "$(rbody 22.95 40.65 23.54 41.08 "$SW1" "$SW2" '"luggage":"suitcase","luggage_count":99')"; want "luggage_count 99" 400
req POST /requests "$S" "$(rbody 22.95 40.65 23.54 41.08 "$SW1" "$SW2" '"luggage":"none","luggage_count":2')"; want "no luggage but a count" 400
req POST /requests "$S" "$(rbody 22.95 40.65 23.54 41.08 "$SW1" "$SW2" '"passenger_id":"'$AU'"')"; want "passenger_id smuggled into the body" 400
req POST /requests "$S" "$(rbody 22.95 40.65 23.54 41.08 "$SW1" "$SW2" '"status":"matched"')"; want "status smuggled into the body" 400
BIG=$(head -c 70000 /dev/zero | tr '\0' 'x')
req POST /requests "$S" "{\"origin_lon\":22.95,\"origin_lat\":40.65,\"destination_lon\":23.54,\"destination_lat\":41.08,\"origin_label\":\"$BIG\",\"destination_label\":\"x\",\"window_start\":\"$SW1\",\"window_end\":\"$SW2\"}"
want "70 KB label (body limit)" '400|413'
L500=$(head -c 500 /dev/zero | tr '\0' 'x')
req POST /requests "$S" "{\"origin_lon\":22.95,\"origin_lat\":40.65,\"destination_lon\":23.54,\"destination_lat\":41.08,\"origin_label\":\"$L500\",\"destination_label\":\"x\",\"window_start\":\"$SW1\",\"window_end\":\"$SW2\"}"
want "500-character label" 400
for lab in $'a\\u202eb' $'a\\u0000b' $'a\\u200bb' $'a\\nb'; do
  req POST /requests "$S" "{\"origin_lon\":22.95,\"origin_lat\":40.65,\"destination_lon\":23.54,\"destination_lat\":41.08,\"origin_label\":\"$lab\",\"destination_label\":\"x\",\"window_start\":\"$SW1\",\"window_end\":\"$SW2\"}"
  case $lab in *202e*) n=RTL-override;; *0000*) n=NUL;; *200b*) n=zero-width-space;; *) n=newline;; esac
  if [ "$n" = newline ]; then want "label with a newline (folded to a space, so accepted)" '200|201'; else want "label with $n" 400; fi
  [ "$CODE" = 201 ] || [ "$CODE" = 200 ] && req DELETE "/requests/$(printf '%s' "$RES" | j .request_id)" "$S" ""
done
req POST /requests "$S" "{\"origin_lon\":22.95,\"origin_lat\":40.65,\"destination_lon\":23.54,\"destination_lat\":41.08,\"origin_label\":\"<script>alert(1)</script>\",\"destination_label\":\"x\",\"window_start\":\"$SW1\",\"window_end\":\"$SW2\"}"
note "label '<script>alert(1)</script>': [$CODE] (markup is stored raw; the clients must escape)"
SREQ=$(printf '%s' "$RES" | j .request_id)
[ -n "$SREQ" ] && [ "$SREQ" != null ] && { req GET /requests/mine "$S"; note "echoed raw: $(printf '%s' "$RES" | jq -c '[.requests[].origin_label]' | head -c 160)"; req DELETE "/requests/$SREQ" "$S" ""; }

section "4d. names, bios, chat: control characters and markup"
req PATCH /me "$A" '{"name":"A‮evil"}';       want "name with RTL override" 400
req PATCH /me "$A" '{"name":"A\u0000B"}';          want "name with NUL" 400
req PATCH /me "$A" "{\"name\":\"$(head -c 300 /dev/zero | tr '\0' 'a')\"}"; want "300-letter name" 400
req PATCH /me "$A" '{"name":"<b>Α</b>"}';          note "name with markup: [$CODE] echoed as $(printf '%s' "$RES" | j .name)"
req PATCH /me "$A" '{"name":"Επιβάτης Α"}'
req PATCH /me "$A" '{"bio":"ring me on 6912345678"}'; want "bio with a phone number" 400
req PATCH /me "$A" '{"bio":"w h a t s a p p  me"}'; note "bio evasion 'w h a t s a p p': [$CODE]"
req PATCH /me "$A" '{"status":"suspended"}';       want "PATCH /me status" 400
req PATCH /me "$A" '{"phone":"+306900009999"}';    want "PATCH /me phone" 400
req PATCH /me "$A" '{"id":"'$BU'"}';               want "PATCH /me id" 400
req POST "/trips/$OCC/messages" "$A" '{"body":"<script>alert(1)</script> ‮ gnirts"}'
want "chat with markup and an RTL override" 201
MID=$(printf '%s' "$RES" | j .id)
[ "$(printf '%s' "$RES" | j .body)" = $'<script>alert(1)</script> \u202e gnirts' ] && ok "the response echoes the markup and the override raw (JSON-escaped on the wire)" || bad "chat echo" "$(excerpt)"
dbeq "stored raw, including U+202E" "SELECT body LIKE '%<script>%' AND body LIKE '%'||chr(8238)||'%' FROM trip_messages WHERE id='$MID'" t
req POST "/trips/$OCC/messages" "$A" '{"body":"nul:\u0000:end"}'
want "chat with a NUL character" 400
lacks "the 400 does not quote the database/driver error" 'store:'
lacks "... or SQLSTATE" 'SQLSTATE'
req POST "/trips/$OCC/messages" "$A" "{\"body\":\"$(head -c 2001 /dev/zero | tr '\0' x)\"}"; want "2001-byte chat" 400
req POST "/trips/$OCC/messages" "$A" "{\"body\":\"$(for i in $(seq 1 600); do printf 'α'; done)\"}"
want "600 Greek letters (1200 bytes, under the 2000 limit)" 201
req POST "/trips/$OCC/messages" "$A" '{"body":"   "}'; want "blank chat" 400

# ===================================================== 5. alternative_of ====
section "5. alternative_of"
DAYS=(); for k in 0 1 2 3 4 5 6 7 8; do DAYS+=("$(TZ=Europe/Athens date -d "$QUIET +$k days" +%F)"); done
alt() { # token dayIndex alternative_of -> CODE RES
  local w1 w2; w1=$(utc "${DAYS[$2]} 07:00"); w2=$(utc "${DAYS[$2]} 10:00")
  req POST /requests "$1" "$(rbody 22.95 40.65 23.54 41.08 "$w1" "$w2" "${3:+\"alternative_of\":$3}")"
}
ALT=$B; ALTU=$BU
alt "$ALT" 0 '';                        want "root day" 201;  R0=$(printf '%s' "$RES" | j .request_id)
alt "$ALT" 1 "\"$R0\"";                 want "another day of it" 201; R1=$(printf '%s' "$RES" | j .request_id)
dbeq "the second day points at the root" "SELECT alternative_of FROM ride_requests WHERE id='$R1'" "$R0"
alt "$ALT" 2 "\"$R1\"";                 want "a day of the second day" 201; R2=$(printf '%s' "$RES" | j .request_id)
dbeq "... joins the root, not the second day (no chain)" "SELECT alternative_of FROM ride_requests WHERE id='$R2'" "$R0"
alt "$ALT" 0 "\"$R0\"";                 want "alternative_of itself (same day again) is the same request" '200'
[ "$(printf '%s' "$RES" | j .request_id)" = "$R0" ] && ok "... and returns the original" || bad "self-alternative" "$LAST -> $(excerpt)"
alt "$ALT" 0 "\"$R2\"";                 want "A->B then B->A: the root's own day, named through its child" '200'
dbeq "no request points at a request that itself has a parent" \
  "SELECT count(*) FROM ride_requests c JOIN ride_requests p ON p.id=c.alternative_of WHERE p.alternative_of IS NOT NULL AND c.passenger_id='$ALTU'" 0
alt "$S" 3 "\"$R0\"";                   want "S points at ALT's request" 404
alt "$S" 3 "\"$RA\"";                   want "S points at A's request (a live ask)" 404
alt "$A" 3 "\"$RA\"";                   want "A points at her own matched request" 409
alt "$A" 3 "\"$RC\"";                   want "A points at C's request" 404
alt "$ALT" 3 '"not-a-uuid"';            want "malformed alternative_of" 400
alt "$ALT" 3 '""';                      want "empty-string alternative_of" 400
alt "$ALT" 3 '12';                      want "numeric alternative_of" 400
alt "$ALT" 3 '[]';                      want "array alternative_of" 400
alt "$ALT" 3 '"00000000-0000-0000-0000-000000000000"'; want "nil-uuid alternative_of" 404
alt "$ALT" 3 '"'"$(printf '%s' "$R0" | tr a-f A-F)"'"'; want "upper-case uuid is the same request" '201|200'
alt "$ALT" 4 "\"$R0\"";  alt "$ALT" 5 "\"$R0\""; alt "$ALT" 6 "\"$R0\""
dbeq "seven live days now" "SELECT count(*) FROM ride_requests WHERE passenger_id='$ALTU' AND status IN ('open','asking') AND COALESCE(alternative_of,id)='$R0'" 7
alt "$ALT" 7 "\"$R0\"";                 want "the eighth day" 409
has "... says too_many_days" too_many_days
alt "$ALT" 7 '';                        note "an eighth day with no alternative_of is a separate journey: [$CODE]"
req DELETE "/requests/$R0" "$ALT"
alt "$ALT" 8 "\"$R0\"";                 want "alternative_of a cancelled root" 409
alt "$ALT" 8 "\"$R1\"";                 note "alternative_of a live child whose root was cancelled: [$CODE] $(excerpt 100)"
# the same, hammered
ALT2=$C2; ALT2U=$C2U
alt "$ALT2" 0 ''; ROOT2=$(printf '%s' "$RES" | j .request_id)
ALTPIDS=()
for k in 1 2 3 4 5 6 7 8; do
  ( w1=$(utc "${DAYS[$k]} 07:00"); w2=$(utc "${DAYS[$k]} 10:00")
    curl -s -o /dev/null -w '%{http_code}\n' -X POST "$API/requests" -H "Authorization: Bearer $ALT2" -H 'Content-Type: application/json' \
      -d "$(rbody 22.95 40.65 23.54 41.08 "$w1" "$w2" "\"alternative_of\":\"$ROOT2\"")" > "$TMP/alt.$k" ) &
  ALTPIDS+=($!)
done; wait "${ALTPIDS[@]}"
dbeq "eight parallel extra days leave at most seven live" "SELECT count(*) <= 7 FROM ride_requests WHERE passenger_id='$ALT2U' AND status IN ('open','asking') AND COALESCE(alternative_of,id)='$ROOT2'" t

# ======================================================== 6b. other probes ==
section "route share link and its public page"
req POST "/me/routes/$TPL/share" "$D"; want "D shares the route" '200|201'
TOK=$(printf '%s' "$RES" | j '.token // (.url|split("/")|last)')
note "share response: $(excerpt 160)"
if [ -n "$TOK" ] && [ "$TOK" != null ]; then
  req GET "/shared-routes/$TOK" ""; want "anonymous read of the shared route" 200
  lacks "... no driver id" "$DU"; lacks "... no driver phone" "$DPHONE"; lacks "... no plate" "${PLATE}D"
  lacks "... no passenger" "$AU"
  note "shared route JSON: $(excerpt 300)"
  curl -s "${API%/v1}/r/$TOK" > "$TMP/page.html"
  grep -qF 'Οδός Μυστική' "$TMP/page.html" && note "the public page prints the driver's street label (typed by the driver)"
fi
req POST "/me/routes/$TPLH/share" "$D"; TOKH=$(printf '%s' "$RES" | j '.token // (.url|split("/")|last)')
if [ -n "$TOKH" ] && [ "$TOKH" != null ]; then
  curl -s "${API%/v1}/r/$TOKH" > "$TMP/pageh.html"
  if grep -qF '<i>x</i>' "$TMP/pageh.html"; then bad "route page escapes driver-typed labels" "GET /r/$TOKH contains raw <i>x</i>"; else ok "route page escapes markup in labels"; fi
fi
req GET /shared-routes/not-a-token ""; want "bogus share token" 404

# =============================================================== 7. auth =====
section "7. authentication"
req GET /me "";                            want "no token" 401
req GET /me "garbage";                     want "garbage token" 401
req GET /me "$A.x";                        want "token with junk appended" 401
req GET /me "" "" -H 'Authorization: Bearer ';  want "empty bearer" 401
req GET /me "" "" -H "Authorization: Basic $A";  want "Basic scheme" 401
HDR=$(printf '%s' "$A" | cut -d. -f1); PAY=$(printf '%s' "$A" | cut -d. -f2); SIG=$(printf '%s' "$A" | cut -d. -f3)
NONE_H=$(printf '{"alg":"none","typ":"JWT"}' | b64e)
req GET /me "$NONE_H.$PAY.";               want "alg=none token" 401
EVIL=$(b64d "$PAY" | jq -c --arg u "$BU" '.sub=$u' | tr -d '\n' | b64e)
req GET /me "$HDR.$EVIL.$SIG";             want "payload swapped to B's id, signature kept" 401
req GET /me "$HDR.$PAY.AAAA";              want "bad signature" 401
note "access token claims: $(b64d "$PAY" | jq -c 'del(.sub)')"
# refresh rotation
login "$SPHONE"; RT=$TOKEN; R1=$REFRESH
req POST /auth/refresh "" "{\"refresh_token\":\"$R1\"}"; want "refresh once" 200
R2=$(printf '%s' "$RES" | j .refresh_token); T2=$(printf '%s' "$RES" | j .access_token)
req POST /auth/refresh "" "{\"refresh_token\":\"$R1\"}"; want "reuse the spent refresh token" 401
req POST /auth/refresh "" "{\"refresh_token\":\"$R2\"}"; want "the family dies with the replay" 401
req GET /me "$T2"; note "the access token from the burned family keeps working until it expires (15 min): [$CODE]"
req POST /auth/refresh "" "{\"refresh_token\":\"$R2\",\"attempt_id\":\"not-a-uuid\"}"; want "malformed attempt_id" '400|401'
req POST /auth/refresh "" '{"refresh_token":""}'; want "empty refresh token" 401
# logout kills the refresh token, not the access token
login "$SPHONE"; LT=$TOKEN; LR=$REFRESH
req POST /auth/logout "" "{\"refresh_token\":\"$LR\"}"; want "logout" 204
req POST /auth/refresh "" "{\"refresh_token\":\"$LR\"}"; want "refresh after logout" 401
req GET /me "$LT"
[ "$CODE" = 401 ] && ok "access token dies at logout" || bad "access token survives logout" "GET /me with the token of a logged-out session -> $CODE (valid until its 15 min expiry; stateless JWT)"
# OTP brute force
PHONE=$BPHONE; next_ip
body=$(curl -s --interface "$IPADDR" -X POST "$API/auth/otp/request" -H 'Content-Type: application/json' -d "{\"phone\":\"$PHONE\"}")
real=$(printf '%s' "$body" | j .dev_code); wrong=$([ "$real" = 000000 ] && echo 111111 || echo 000000)
codes=""; for i in 1 2 3 4 5 6; do req POST /auth/otp/verify "" "{\"phone\":\"$PHONE\",\"code\":\"$wrong\"}"; codes="$codes $CODE"; done
req POST /auth/otp/verify "" "{\"phone\":\"$PHONE\",\"code\":\"$real\"}"
[ "$CODE" = 401 ] && ok "the right code is refused after 5 wrong ones ($codes then 401)" || bad "OTP attempt limit" "after 6 wrong tries the real code was accepted: $CODE"
req POST /auth/otp/request "" "{\"phone\":\"$PHONE\"}" ; want "second code for the same number inside the cooldown" '200|204|429'
note "re-request: [$CODE]"
section "7c. phone-change as a registry oracle, photos"
req POST /me/phone/request "$A" "{\"phone\":\"$DPHONE\"}"; c_taken=$CODE; b_taken=$(excerpt 90)
req POST /me/phone/request "$A" '{"phone":"+306900009998"}'; c_free=$CODE
if [ "$c_taken" = "$c_free" ]; then ok "a taken number and a free one are answered alike [$c_taken]"
else
  n=0; for ph in "$DPHONE" "$EPHONE" "$BPHONE" "$SPHONE" "$DPHONE" "$EPHONE" "$BPHONE" "$SPHONE" "$DPHONE" "$EPHONE" "$BPHONE" "$SPHONE"; do
    req POST /me/phone/request "$A" "{\"phone\":\"$ph\"}"; [ "$CODE" = 409 ] && n=$((n+1)); done
  bad "POST /me/phone/request tells any signed-in user whether a number has an account" "taken number -> $c_taken $b_taken, free number -> $c_free; $n/12 further probes of registered numbers all answered 409 phone_unavailable with no rate limit (ErrPhoneTaken is raised before withinLimits in internal/auth/phone_change.go Request)"
fi
PNG=iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==
printf '%s' "$PNG" | base64 -d > "$TMP/p.png"
curl -s -o "$TMP/res" -w '%{http_code}' -X PUT "$API/me/photo" -H "Authorization: Bearer $A" -H 'Content-Type: image/png' --data-binary @"$TMP/p.png" > "$TMP/code"; PHC=$(cat "$TMP/code")
note "A uploads a 1x1 PNG: [$PHC]"
curl -s -o "$TMP/res" -w '%{http_code}' -X PUT "$API/me/photo" -H "Authorization: Bearer $A" -H 'Content-Type: image/png' --data-binary '<html><script>alert(1)</script></html>' > "$TMP/code"
[ "$(cat "$TMP/code")" = 400 ] && ok "HTML sent as a photo is refused" || bad "HTML accepted as a photo" "PUT /me/photo -> $(cat "$TMP/code")"
curl -s -o "$TMP/res" -w '%{http_code}' -X PUT "$API/me/photo" -H "Authorization: Bearer $A" -H 'Content-Type: image/svg+xml' --data-binary '<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>' > "$TMP/code"
[ "$(cat "$TMP/code")" = 400 ] && ok "SVG sent as a photo is refused" || bad "SVG accepted as a photo" "PUT /me/photo -> $(cat "$TMP/code")"
if [ "$PHC" = 200 ]; then
  curl -s -o /dev/null -w '%{http_code}' "$API/users/$AU/photo" -H "Authorization: Bearer $S" > "$TMP/code"
  note "S (never on a trip with A) fetches A's photo by uuid: [$(cat "$TMP/code")] (by design: any signed-in user holding the uuid)"
  curl -s -o /dev/null -w '%{http_code}' "$API/users/$AU/photo" > "$TMP/code"
  [ "$(cat "$TMP/code")" = 401 ] && ok "a photo needs a token" || bad "photo served without a token" "GET /users/$AU/photo -> $(cat "$TMP/code")"
fi
req DELETE /me/photo "$A" >/dev/null

# deleted account
section "7b. account deletion"
req GET /me "$Z"; want "Z is alive" 200
req DELETE /me "$Z" ; note "DELETE /me with no confirmation body: [$CODE] $(excerpt 120)"
if [ "$CODE" = 400 ] || [ "$CODE" = 422 ]; then req DELETE /me "$Z" '{"confirm":true}'; note "DELETE /me with a confirm body: [$CODE] $(excerpt 120)"; fi
req GET /me "$Z";                 want "deleted account's access token" '401|403'
req GET /asks/pending "$Z";       want "deleted account lists asks" '401|403'
req POST /requests "$Z" "$(rbody 22.95 40.65 23.54 41.08 "$SW1" "$SW2")"; want "deleted account posts a request" '401|403'
req POST /auth/refresh "" "{\"refresh_token\":\"$ZREFRESH\"}"; want "deleted account's refresh token" 401
dbeq "deleted account is anonymised (no phone left)" "SELECT coalesce(phone,'') NOT LIKE '+3069%' FROM users WHERE id='$ZU'" t
dbeq "... name erased" "SELECT name <> 'Διαγραφή Θ' FROM users WHERE id='$ZU'" t
note "suspended-user behaviour needs a moderator or a DB write; covered by internal/api middleware tests, not driven here"

# ================================================ 8. rate limits (<=100) ====
section "8. expensive endpoints without a limit (note only; about 90 requests in total)"
t0=$(date +%s%N); n429=0
for i in $(seq 1 10); do req GET "/places?q=%CE%98%CE%B5%CF%83&limit=50" ""; [ "$CODE" = 429 ] && n429=$((n429+1)); done
note "10 anonymous /places searches: $n429 x 429, $(( ($(date +%s%N) - t0) / 1000000 )) ms"
n429=0; for i in $(seq 1 5); do req POST /places/near "" '{"lon":22.94,"lat":40.64}'; [ "$CODE" = 429 ] && n429=$((n429+1)); done
note "5 anonymous /places/near: $n429 x 429"
n429=0; t0=$(date +%s%N)
for i in $(seq 1 10); do req POST /estimates "$S" '{"origin_lon":22.9444,"origin_lat":40.6401,"destination_lon":23.548,"destination_lat":41.0855}'; [ "$CODE" = 429 ] && n429=$((n429+1)); done
note "10 /estimates (each an OSRM route): $n429 x 429, $(( ($(date +%s%N) - t0) / 1000000 )) ms, last [$CODE]"
n429=0; for i in $(seq 1 5); do req POST /supply "$S" "{\"origin_lon\":22.9444,\"origin_lat\":40.6401,\"destination_lon\":23.548,\"destination_lat\":41.0855,\"date\":\"$TODAY\"}"; [ "$CODE" = 429 ] && n429=$((n429+1)); done
note "5 /supply: $n429 x 429 (limit is 30/min/user)"
# X-Real-IP is trusted from the client
last=0; for i in $(seq 1 62); do req GET /shared-routes/zzz "" "" -H 'X-Real-IP: 10.99.99.9'; last=$CODE; done
req GET /shared-routes/zzz "" "" -H 'X-Real-IP: 10.99.99.10'
if [ "$last" = 429 ] && [ "$CODE" = 404 ]; then
  bad "per-IP limits key on a client-supplied X-Real-IP" "62 x GET /shared-routes/zzz with X-Real-IP: 10.99.99.9 -> last $last; same TCP peer with X-Real-IP: 10.99.99.10 -> $CODE. Behind a proxy that overwrites the header this is fine; on a directly reachable port (dev-lan, a VPS without Caddy) it lets anyone dodge the OTP-per-IP SMS-pumping cap too"
else ok "per-IP limit does not honour a spoofed X-Real-IP (last=$last, other=$CODE)"; fi

# ======================================= phase 2: after the trip has left ====
section "waiting for $LH (the end of the departure window) to pass"
while [ "$(date +%s)" -le $((L1 + 4)) ]; do sleep 5; done

section "3b/4e. confirm, rate, expired and closed asks"
# not-yet cases were run above; these need the clock to have passed
req POST "/trips/$OCC/confirm" "$S";   want "S confirms a trip it is not on" 404
req POST "/trips/$OCC/confirm" "$B";   want "B (cancelled) confirms" 404
req POST "/trips/$OCC/rate" "$S" '{"stars":5}'; want "S rates" 404
req POST "/trips/$OCC/rate" "$B" '{"stars":1}'; want "B (cancelled) rates the driver" 404
req POST "/trips/$OCC/rate" "$A" '{"stars":5}'; want "A rates before the ride is completed (only she has not confirmed)" 409
req POST "/trips/$OCC/confirm" "$A";   want "A confirms" 204
req POST "/trips/$OCC/confirm" "$A";   want "A confirms twice" 204
req POST "/trips/$OCC/rate" "$A" '{"stars":5}'; want "A rates before D confirmed" 409
req POST "/trips/$OCC/rate" "$D" "{\"stars\":5,\"ratee_id\":\"$AU\"}"; want "D rates A before the ride is completed" 409
req POST "/trips/$OCC/confirm" "$D";   want "D confirms" 204
dbeq "A's booking is completed" "SELECT status FROM bookings WHERE id='$BA'" completed
for s in 0 6 -1 '"5"' 2.5; do req POST "/trips/$OCC/rate" "$A" "{\"stars\":$s}"; want "stars $s" 400; done
req POST "/trips/$OCC/rate" "$A" "{\"stars\":5,\"ratee_id\":\"$AU\"}"; want "A rates herself" '400|409'
dbeq "no self-rating row" "SELECT count(*) FROM ratings WHERE rater_id=ratee_id" 0
req POST "/trips/$OCC/rate" "$D" "{\"stars\":5,\"ratee_id\":\"$DU\"}"; want "D rates himself" '400|409'
req POST "/trips/$OCC/rate" "$D" "{\"stars\":5,\"ratee_id\":\"$BU\"}"; want "D rates B, whose booking was cancelled" '404|409'
req POST "/trips/$OCC/rate" "$D" "{\"stars\":5,\"ratee_id\":\"$SU\"}"; want "D rates S, who was never aboard" '404|409'
req POST "/trips/$OCC/rate" "$A" "{\"stars\":5,\"ratee_id\":\"$SU\"}"; want "A names a ratee other than the driver" '400|409'
HUGE=$(head -c 40000 /dev/zero | tr '\0' 'x')
req POST "/trips/$OCC/rate" "$A" "{\"stars\":4,\"tag\":\"$HUGE\"}"
if [ "$CODE" = 201 ]; then bad "rating tag is unbounded and unvalidated" "POST /trips/$OCC/rate {stars:4, tag:<40000 x>} -> 201; ratings.tag is text with no length check ($(q "SELECT length(tag) FROM ratings WHERE rater_id='$AU'") chars stored)"
else ok "oversized rating tag refused [$CODE]"; fi
req POST "/trips/$OCC/rate" "$A" '{"stars":5}'; want "A rates the driver a second time" 409
req POST "/trips/$OCC/rate" "$D" "{\"stars\":5,\"ratee_id\":\"$AU\",\"tag\":\"<b>ok</b>\"}"; want "D rates A" 201
req POST "/trips/$OCC/rate" "$D" "{\"stars\":1,\"ratee_id\":\"$AU\"}"; want "D rates A twice" 409
parallel 5 POST "/trips/$OCC/rate" "$D" "{\"stars\":2,\"ratee_id\":\"$AU\"}"; note "5 parallel re-ratings: $PCODES"
dbeq "one rating each way" "SELECT count(*) FROM ratings WHERE booking_id='$BA'" 2
req POST "/trips/$OCCE/rate" "$A" '{"stars":5}'; want "A rates a trip she was never on" 404
req POST "/trips/$OCCE/rate" "$E" "{\"stars\":5,\"ratee_id\":\"$CU\"}"; want "E rates a passenger of a ride that did not happen (C's request was cancelled or not completed)" '404|409'
# cancelling after departure
req DELETE "/bookings/$BA" "$A";  want "A cancels a completed ride" '204|409'
dbeq "the completed booking stays completed" "SELECT status FROM bookings WHERE id='$BA'" completed
req DELETE "/trips/$OCC" "$D";    want "D cancels a trip that has left" '409'
# asks on E: expired / declined
sleep 1
req POST "/asks/$AC2/accept" "$E"
want "E accepts C2's ask after it expired" '409'
ST=$(q "SELECT status FROM asks WHERE id='$AC2'")
if printf '%s' "$RES" | grep -q ask_expired; then ok "... and says ask_expired"
else bad "an expired ask answers 'already been answered'" "POST /asks/<expired>/accept -> $CODE $(excerpt 100); asks.status='$ST' (the sweeper had already flipped it; before it does, the same call says ask_expired)"; fi
req POST "/asks/$AC2/decline" "$E"; note "decline of an expired ask: [$CODE] $(excerpt 100)"
req GET "/trips/$OCC" "$A"
has "A's own trip view lists her as completed" '"can_rate":false'
dbeq "no ask is pending after the departure" "SELECT count(*) FROM asks WHERE occurrence_id IN ('$OCC','$OCCE') AND status='pending' AND expires_at < now()" 0

section "cancelled ask"
req POST "/asks/$AC/accept" "$E";   note "E accepts C's ask after the cancel/accept race: [$CODE] $(excerpt 100)"
dbeq "E never booked C against C's cancelled request" "SELECT count(*) FROM bookings b JOIN ride_requests r ON r.id=b.request_id WHERE r.id='$RC' AND r.status='cancelled' AND b.status='confirmed'" 0

section "live map after departure"
req GET /live/map "$S"
want "S reads the live map" 200
note "map items: $(printf '%s' "$RES" | jq -c '[.trips[]|keys]|unique' 2>/dev/null)"
lacks "no driver id" "$DU"
lacks "no street label" Μυστική
lacks "no plate" "${PLATE}D"
lacks "no passenger id" "$AU"

section "live positions after the trip"
req PUT "/trips/$OCC/position" "$A" '{"lat":40.9,"lon":23.3}'
note "A uploads after her ride is completed (window open until the trip's end + 1 h): [$CODE]"
req GET "/trips/$OCC" "$B"; want "B still 404 after the ride" 404

printf '\n%d ok, %d FAIL\n' "$PASSES" "$FAILS"
[ "$FAILS" -gt 100 ] && FAILS=100
exit "$FAILS"

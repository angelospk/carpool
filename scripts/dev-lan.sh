#!/usr/bin/env bash
# Bring the whole thing up on the LAN, so it can be opened from a phone or a laptop.
#
#   scripts/dev-lan.sh
#
# Nothing here survives a reboot — not the containers, not the API, not the
# static server — and a machine that restarts overnight leaves a page that loads
# from browser cache and fails on the first request with a generic error. That
# is a confusing ten minutes, so this exists to be re-run rather than debugged.
#
# It is idempotent: containers are started if stopped, the API is replaced if
# already running, the web bundle is rebuilt. Accounts and routes are left
# alone; use scripts/seed-demo.sh for those.
set -euo pipefail

cd "$(dirname "$0")/.."

# The address the phone will use. Explicitly not localhost: the bundle has the
# API URL compiled into it, and a bundle built against localhost is useless on
# any other device.
IP=${IP:-$(hostname -I | awk '{print $1}')}
API_PORT=${API_PORT:-8080}
WEB_PORT=${WEB_PORT:-8099}
DB=${DB:-lan}
WEB_DIR=${WEB_DIR:-/tmp/carpool-web}

step() { printf '\n\033[1m%s\033[0m\n' "$*"; }

step "backing services"
docker compose -f docker-compose.dev.yml up -d
# Compose returns once the containers are started, not once Postgres is ready to
# answer, and the API exits rather than waits if it cannot connect.
for _ in $(seq 30); do
  docker compose -f docker-compose.dev.yml ps --format '{{.Health}}' | grep -qv healthy || break
  sleep 2
done
echo "   postgis + osrm healthy"

step "database $DB"
psql "postgres://carpool:carpool@localhost:5433/postgres?sslmode=disable" -tAc \
  "SELECT 1 FROM pg_database WHERE datname='$DB'" | grep -q 1 ||
  psql "postgres://carpool:carpool@localhost:5433/postgres?sslmode=disable" -c "CREATE DATABASE $DB"
echo "   ready (migrations run at API boot)"

step "api on $IP:$API_PORT"
pkill -f 'bin/api' 2>/dev/null || true
go build -o bin/api ./cmd/api
DATABASE_URL="postgres://carpool:carpool@localhost:5433/$DB?sslmode=disable" \
JWT_SECRET="lan-dev-secret-that-is-long-enough-32b" \
DEV_MODE=true HTTP_ADDR="0.0.0.0:$API_PORT" \
REGION_FIXTURE=fixtures/regions/evros.geojson \
  setsid ./bin/api < /dev/null > /tmp/api-lan.log 2>&1 &
disown
for _ in $(seq 20); do
  curl -fs -o /dev/null "http://$IP:$API_PORT/v1/regions" 2>/dev/null && break
  sleep 1
done
curl -fsS -o /dev/null "http://$IP:$API_PORT/v1/regions" ||
  { echo "the API did not come up; see /tmp/api-lan.log" >&2; exit 1; }
echo "   up, DEV_MODE, log at /tmp/api-lan.log"

step "web bundle"
# --clear is not optional. Metro caches aggressively and will happily rebuild in
# under a second while shipping yesterday's API URL, which then fails on a
# device with no visible cause. And the URL comes from app/.env: `expo export`
# does not read EXPO_PUBLIC_* out of the environment.
printf 'EXPO_PUBLIC_API_URL=http://%s:%s\n' "$IP" "$API_PORT" > app/.env
(cd app && npx expo export --platform web --clear --output-dir "$WEB_DIR" > /tmp/expo-export.log 2>&1) ||
  { echo "the export failed; see /tmp/expo-export.log" >&2; exit 1; }
pkill -f "http.server $WEB_PORT" 2>/dev/null || true
# setsid, not just nohup: a plain background job stays a child of this script,
# and bash then waits for it at exit, so the script never returns and anything
# reading its output hangs with everything apparently working.
(cd "$WEB_DIR" && setsid python3 -m http.server "$WEB_PORT" --bind 0.0.0.0 \
  < /dev/null > /tmp/carpool-web.log 2>&1 &)
disown -a 2>/dev/null || true
sleep 2

cat <<TXT

$(printf '\033[1mOpen http://%s:%s\033[0m' "$IP" "$WEB_PORT")

Sign in with any seeded number — the code fills itself in, so tap through.
Type the number in full, including +30; the field starts at +30 and the
button stays dead until the whole thing is there.

  +306900000101   Γιώργος   driver
  +306900000102   Μαρία     driver
  +306900000103   Νίκος     passenger

No accounts yet? Run scripts/seed-demo.sh.
Stop everything: pkill -f bin/api; pkill -f http.server; docker compose -f docker-compose.dev.yml down
TXT

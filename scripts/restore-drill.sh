#!/usr/bin/env bash
# Restore a dump into a scratch database and verify it came back intact.
#
#   scripts/restore-drill.sh backups/carpool-20260729T031500Z.dump
#
# This is the half of a backup strategy people skip. It restores into a
# throwaway database, compares row counts against the live one, and checks that
# the spatial indexes and a representative PostGIS query survived.
set -euo pipefail

DUMP=${1:?usage: restore-drill.sh <dump file>}
SERVICE=${SERVICE:-db}
USER=${USER_NAME:-carpool}
SCRATCH=${SCRATCH:-carpool_restore_drill}

psql_scratch() { docker compose exec -T "$SERVICE" psql -U "$USER" -d "$SCRATCH" -tAc "$1"; }

echo "==> creating scratch database $SCRATCH"
docker compose exec -T "$SERVICE" psql -U "$USER" -d postgres -c "DROP DATABASE IF EXISTS $SCRATCH WITH (FORCE)"
docker compose exec -T "$SERVICE" psql -U "$USER" -d postgres -c "CREATE DATABASE $SCRATCH"

echo "==> restoring"
docker compose exec -T "$SERVICE" pg_restore -U "$USER" -d "$SCRATCH" --no-owner < "$DUMP"

echo "==> comparing row counts against the live database"
fail=0
for table in regions users driver_profiles route_templates route_schedules \
             trip_occurrences ride_requests asks bookings ratings trip_messages; do
	live=$(docker compose exec -T "$SERVICE" psql -U "$USER" -d carpool -tAc "SELECT count(*) FROM $table")
	restored=$(psql_scratch "SELECT count(*) FROM $table")
	if [ "$live" = "$restored" ]; then
		printf '    %-18s %s\n' "$table" "$restored"
	else
		printf '    %-18s live=%s restored=%s  MISMATCH\n' "$table" "$live" "$restored" >&2
		fail=1
	fi
done

echo "==> checking the spatial indexes came back"
for index in route_templates_geog_gist trip_occurrences_geog_gist regions_boundary_gist; do
	found=$(psql_scratch "SELECT count(*) FROM pg_indexes WHERE indexname = '$index'")
	[ "$found" = "1" ] || { echo "    missing index $index" >&2; fail=1; }
done

echo "==> checking a representative spatial query still runs"
psql_scratch "SELECT count(*) FROM route_templates
              WHERE ST_DWithin(route_geom::geography,
                               ST_SetSRID(ST_MakePoint(25.87, 40.85), 4326)::geography, 2000)" >/dev/null

echo "==> dropping the scratch database"
docker compose exec -T "$SERVICE" psql -U "$USER" -d postgres -c "DROP DATABASE $SCRATCH WITH (FORCE)"

if [ "$fail" -ne 0 ]; then
	echo "restore drill FAILED" >&2
	exit 1
fi
echo "restore drill passed"

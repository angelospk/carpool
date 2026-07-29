#!/usr/bin/env bash
# Nightly logical backup of the carpool database.
#
#   scripts/backup.sh                 # write a dump to ./backups
#   BACKUP_DIR=/mnt/x scripts/backup.sh
#
# Run from cron on the VPS:
#   15 3 * * * cd /srv/carpool && scripts/backup.sh >> /var/log/carpool-backup.log 2>&1
#
# The dump is custom-format (-Fc), which restores with pg_restore, supports
# parallel restore, and compresses. Test the restore with scripts/restore-drill.sh
# — an untested backup is not a backup.
set -euo pipefail

# COMPOSE_FILE selects the stack; the dev one is useful for rehearsing this.
COMPOSE=(docker compose ${COMPOSE_FILE:+-f "$COMPOSE_FILE"})

BACKUP_DIR=${BACKUP_DIR:-./backups}
KEEP_DAYS=${KEEP_DAYS:-14}
SERVICE=${SERVICE:-db}
DB=${DB:-carpool}
USER=${USER_NAME:-carpool}

mkdir -p "$BACKUP_DIR"
stamp=$(date -u +%Y%m%dT%H%M%SZ)
out="$BACKUP_DIR/carpool-$stamp.dump"

echo "==> dumping $DB to $out"
"${COMPOSE[@]}" exec -T "$SERVICE" pg_dump -U "$USER" -d "$DB" -Fc > "$out"

# A dump that is suspiciously small usually means the command failed but the
# redirect still created a file.
size=$(stat -c%s "$out")
if [ "$size" -lt 4096 ]; then
	echo "!! dump is only ${size} bytes; treating as failed" >&2
	rm -f "$out"
	exit 1
fi

echo "==> wrote $(du -h "$out" | cut -f1)"

echo "==> pruning dumps older than ${KEEP_DAYS} days"
find "$BACKUP_DIR" -name 'carpool-*.dump' -mtime "+${KEEP_DAYS}" -print -delete

echo "==> done"

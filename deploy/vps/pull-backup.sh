#!/usr/bin/env bash
# Pulls a dump of the VM's database to this machine and keeps the last 30.
# Run nightly by the carpool-backup systemd user timer on the mini PC.
set -euo pipefail

DIR=${CARPOOL_BACKUP_DIR:-$HOME/backups/carpool}
KEY=${CARPOOL_BACKUP_KEY:-$HOME/.config/carpool/vps_backup}
HOSTS=${CARPOOL_KNOWN_HOSTS:-$HOME/.config/carpool/vps_known_hosts}

mkdir -p "$DIR"
out="$DIR/carpool-$(date +%Y%m%d-%H%M).dump"
tmp="$out.part"
ssh -i "$KEY" -o IdentitiesOnly=yes -o UserKnownHostsFile="$HOSTS" -o StrictHostKeyChecking=yes \
  deploy@79.76.114.184 backup >"$tmp"

# A custom-format dump starts with PGDMP. Anything else is an error message
# or a truncated stream, and must not push a good dump out of the 30 kept.
[[ $(head -c 5 "$tmp") == PGDMP ]] || { echo "not a pg_dump: $tmp" >&2; exit 1; }
mv "$tmp" "$out"
ls -1t "$DIR"/carpool-*.dump | tail -n +31 | xargs -r rm -f
echo "backup: $out ($(stat -c %s "$out") bytes)"

#!/usr/bin/env bash
# Build OSRM MLD artifacts from an OSM extract.
#
# Preprocessing needs far more RAM than serving, so it runs off the VPS (on the
# mini PC) per DEPLOYMENT.md. Only the resulting .osrm* files ship to the server.
set -euo pipefail

PBF="${1:?usage: build-osrm.sh <extract.osm.pbf>}"
PROFILE="${OSRM_PROFILE:-$(brew --prefix osrm-backend)/share/osrm/profiles/car.lua}"
BASE="${PBF%.osm.pbf}"

echo "==> extract  ($(date -Is))"
osrm-extract -p "$PROFILE" "$PBF"
echo "==> partition ($(date -Is))"
osrm-partition "${BASE}.osrm"
echo "==> customize ($(date -Is))"
osrm-customize "${BASE}.osrm"
echo "==> done      ($(date -Is))"
ls -lh "${BASE}".osrm* | tail -5

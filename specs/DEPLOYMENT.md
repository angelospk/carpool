# Deployment

## Target

One small VPS (2 vCPU / 2–4 GB RAM), Docker Compose. Everything self-hosted,
open-source reproducible: `git clone && cp .env.example .env && docker compose up`.

## Compose services

```yaml
services:
  api:        # Go binary, scratch/distroless image, ~30-80 MB RSS
    build: .
    depends_on: [db, osrm]
    environment: [DATABASE_URL, OSRM_URL, EXPO_PUSH..., SMS_...]
  db:         # postgres:16 + postgis:3.4 (imresamu/postgis or postgis/postgis)
    volumes: [pgdata:/var/lib/postgresql/data]
    # tuned small: shared_buffers 256MB, work_mem 16MB, max_connections 30
  osrm:       # ghcr.io/project-osrm/osrm-backend, MLD, car profile
    command: osrm-routed --algorithm mld /data/region.osrm
    volumes: [./osrm-data:/data:ro]
  caddy:      # TLS + reverse proxy (automatic Let's Encrypt)
```

RAM budget (verify with real measurements — Codex flag):
API ~100 MB + Postgres ~400 MB + OSRM (regional extract) ~200–500 MB + Caddy ~30 MB
→ comfortably under 2 GB. **Benchmark actual OSRM RSS with the real extract before
committing to VPS size.**

## OSRM data pipeline (runs OFF the VPS)

Preprocessing needs far more RAM than serving — run it on the mini PC (60 GB RAM):

```bash
# 1. Get regional extract (Geofabrik: greece-latest, or clip to prefecture + margin)
wget https://download.geofabrik.de/europe/greece-latest.osm.pbf
# Optional clip (osmium) to prefecture polygon + ~30 km buffer:
osmium extract -p region-buffered.geojson greece-latest.osm.pbf -o region.osm.pbf
# 2. Build MLD artifacts
docker run -v $PWD:/data ghcr.io/project-osrm/osrm-backend \
  osrm-extract -p /opt/car.lua /data/region.osm.pbf
docker run -v $PWD:/data ghcr.io/project-osrm/osrm-backend osrm-partition /data/region.osrm
docker run -v $PWD:/data ghcr.io/project-osrm/osrm-backend osrm-customize /data/region.osrm
# 3. rsync artifacts to VPS ./osrm-data/, restart osrm container
```

- Refresh cadence: quarterly (or on notable OSM edits). Stamp
  `osrm_data_version` = extract date; API re-snaps stored routes after swap.
- Clip margin matters: routes legitimately exit the prefecture — buffer
  generously (≥ 30 km) or just use the whole-Greece extract if RAM allows.

## Region boundaries

Seed `regions.boundary` from OSM admin boundaries (level 5/6) via overpass or
Geofabrik shapefiles; check into the repo as GeoJSON fixtures.

## Operations

- **Migrations**: run by the API container at boot (goose), guarded by advisory lock.
- **Backups**: nightly `pg_dump` → restic to any S3-compatible bucket (or Syncthing
  to the mini PC for the pilot). Test-restore documented in the repo.
- **Monitoring**: `/metrics` Prometheus endpoint; for the pilot a simple
  healthcheck ping (uptime-kuma on the mini PC) is enough.
- **Logs**: stdout → `docker logs`; loki optional later.
- **Secrets**: `.env` file on VPS, never in git. SMS + Expo credentials only.

## Environments

- **dev**: everything on the mini PC (compose file identical, dev OTP printed to
  console instead of SMS; Expo dev-build pointed at `minipc.tail11f3d4.ts.net`).
- **prod**: VPS + real SMS provider + EAS builds.

## CI (GitHub Actions)

- Go: vet, staticcheck, tests (with dockerized postgis via testcontainers),
  build image.
- Mobile: tsc + eslint + jest; EAS build triggered manually.
- License: AGPL-3.0 for the server (keeps hosted forks open), MIT for the app —
  confirm before first public release.

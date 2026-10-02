# The Oracle VM

`carpool.haroldpoi.dev` runs on the Oracle Cloud VM (E2.1.Micro: 1 GB RAM, 2
vCPU, x86_64), next to Vaultwarden. SSH as `ubuntu` goes through the MacBook
(`ssh oracle-vm`); the public port 22 only lets CI's `deploy` user in.

```
Cloudflare tunnel ──> Caddy 127.0.0.1:8180 ──> /get        APK in /opt/carpool/apk
                                           └─> everything  API 127.0.0.1:8187
                                                            ├─ PostGIS (volume carpool_pgdata)
                                                            └─ OSRM (/opt/carpool/osrm-data)
```

## How a change gets there

- **API**: a push to master that touches Go code runs `deploy-api`. CI builds
  the image (the VM cannot: a Go build does not fit in 1 GB), pipes
  `docker save` over SSH, and `carpool-deploy` loads it, restarts the API and
  waits for `/v1/readyz`. Not ready within 90 s means the previous image goes
  back. Migrations run at boot, so a deploy cannot roll back a schema: keep
  migrations additive.
- **APK**: a push that touches `app/` runs `build-apk`, which publishes a release
  APK signed with our own key and `versionCode` = the run number.
  `https://carpool.haroldpoi.dev/get` serves the newest.

## What lives where

| What | Where |
|---|---|
| Stack | `/opt/carpool/compose.yml` (this directory's `compose.yml`) |
| Secrets | `/opt/carpool/.env`, root-only. A copy is on the mini PC in `~/.config/carpool/vps.env` |
| Running image | `/opt/carpool/tag.env` |
| Deploy script | `/usr/local/bin/carpool-deploy`, run via the forced-command key in `/home/deploy/.ssh/authorized_keys` |
| Backup script | `/usr/local/bin/carpool-backup`, the second key in the same file |
| Caddy | `/etc/caddy/Caddyfile` (this directory's `Caddyfile`) |
| Tunnel | `/etc/cloudflared/config.yml`, ingress `carpool.haroldpoi.dev` → `http://127.0.0.1:8180` |
| APK signing key | GitHub secrets, and `~/.config/carpool/release.keystore` on the mini PC. Lose it and installed apps can no longer be updated. |

## Backups

Nightly at 03:30 the mini PC pulls a `pg_dump` into `~/backups/carpool/` and
keeps the last 30 (`carpool-backup.timer`, a systemd user timer running
`pull-backup.sh`). It uses its own key, `~/.config/carpool/vps_backup`, forced
to `carpool-backup`: CI's deploy key cannot read the data, and this key cannot
ship code. Restore with `pg_restore -d <db> <file>`.

`compose.yml`, `Caddyfile`, `carpool-deploy` and `carpool-backup` are **not** synced by CI. A
change to them is copied by hand; letting CI write a compose file would let a
leaked deploy key mount anything on the host.

## By hand

```bash
ssh oracle-vm
cd /opt/carpool
C="sudo docker compose --env-file .env --env-file tag.env"
$C ps
$C logs -f api
# roll back to an earlier image
sudo docker image ls carpool-api
echo API_TAG=<sha> | sudo tee tag.env && $C up -d
```

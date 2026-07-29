# Carpool — local carpooling, open source

Working name: **carpool** (final name TBD). A local-first carpooling app scoped to a
region/prefecture (νομός): drivers publish recurring or one-off routes, passengers
request trips, the backend matches by route overlap + detour budget and asks drivers
via push notification. Cost sharing is a suggestion (fuel formula), not a payment.

**Non-goals:** not a ride-hailing service, no in-app payments, no national scale.

## Stack (decided 2026-07-29, Codex-reviewed)

| Layer | Choice | Why |
| --- | --- | --- |
| Mobile | React Native **Expo** (expo-router) | Required: must be an app; fastest solo iteration |
| Backend | **Go**, single binary (chi, pgx, coder/websocket) | Tiny footprint, dev velocity > Rust's 2-4x memory edge at this scale |
| Datastore | **PostgreSQL + PostGIS** (only datastore) | Handles geo matching + outbox + everything; no Redis/H3/Kafka under ~10k users |
| Routing | Self-hosted **OSRM**, Greek regional extract, Docker sidecar | Lightest/fastest for car-only; BSD license |
| Push | Expo Push API (direct HTTP client in Go) | No third-party dependency ambiguity |
| Hosting | Single small VPS, target **< 2 GB RAM total** | Cheap self-hosting, open-source deployability |

## Spec files

- [specs/SPEC.md](specs/SPEC.md) — product spec: personas, flows, features, settings
- [specs/ARCHITECTURE.md](specs/ARCHITECTURE.md) — system architecture, services, realtime/event design
- [specs/DATA-MODEL.md](specs/DATA-MODEL.md) — schema: users, route templates, schedules, occurrences, requests, matches, outbox
- [specs/MATCHING.md](specs/MATCHING.md) — 2-stage matching algorithm (PostGIS gross filter → OSRM fine detour)
- [specs/MOBILE.md](specs/MOBILE.md) — Expo app: screens, libraries, background location, push handling
- [specs/DEPLOYMENT.md](specs/DEPLOYMENT.md) — VPS layout, Docker compose, OSRM data pipeline, backups
- [specs/RISKS.md](specs/RISKS.md) — open risks and deferred decisions

## Provenance

Distilled from Harold's vault notes (obs: daily/2025-05-16 idea dump + real-time
location tracking spec), web research (BlaBlaCar Zen matching architecture, routing
engine comparisons 2026, Expo maps ecosystem), and a Codex plan review
(2026-07-29) whose corrections are folded in: direction-aware gross matching,
outbox-based event delivery, materialized trip occurrences.

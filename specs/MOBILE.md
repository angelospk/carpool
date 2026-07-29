# Mobile App (Expo)

## Stack

| Concern | Library | Notes |
| --- | --- | --- |
| Framework | Expo SDK (latest stable) + **expo-router** | File-based routing; dev-builds required (not Expo Go) because of push + background location |
| Maps | **react-native-maps** (MVP) | Battle-tested; Apple Maps on iOS (free), Google Maps on Android. Migrate to MapLibre RN + self-hosted vector tiles only if Google costs/API keys become a pain |
| Location | expo-location (+ expo-task-manager post-MVP) | MVP needs foreground only (pick points, show position). Background tracking only for the post-MVP live-ride feature |
| Push | expo-notifications | Expo push tokens registered at login; server sends via Expo Push HTTP API |
| State/server | @tanstack/react-query + small zustand store | offlineFirst networkMode; reconcile-on-focus |
| Storage | expo-secure-store (JWT) + AsyncStorage (prefs/cache) | |
| Realtime | native WebSocket in foreground | reconnect w/ backoff; every reconnect refetches affected queries |
| i18n | i18next, Greek default + English | copy the pattern from the OpenPopcorn review notes |
| Polyline | @mapbox/polyline decode | routes come from server as polyline6 |

## Screens (expo-router tree)

```
app/
  (auth)/phone.tsx           # phone input → OTP
  (auth)/otp.tsx
  (auth)/profile-setup.tsx   # name, photo, region, driver? car details
  (tabs)/index.tsx           # Home: upcoming trips (driver+passenger merged)
  (tabs)/search.tsx          # passenger: origin/dest/time → results | post open request
  (tabs)/publish.tsx         # driver: route wizard (map → schedule → seats/prefs → confirm)
  (tabs)/profile.tsx         # me, car, prefs, settings
  trip/[id].tsx              # trip card: route map, people, price, chat, actions
  request/[id].tsx           # open request status: asking driver N…, cancel
  ask/[id].tsx               # driver ask screen: map w/ detour overlay, +X min, accept/decline
  user/[id].tsx              # public profile, ratings
```

## Key UX details

- **Ask screen is the heart of the driver experience**: opened from push, shows
  original route + detoured route overlay, "+6 min, +2.1 km, 3.50 € suggested",
  two big buttons. Must load in one round-trip (`GET /asks/{id}` returns
  everything incl. both polylines).
- Publish wizard defaults aggressively (seats 3, detour 10 min, radius 2 km) —
  target: recurring route published in < 90 s.
- Search results show: departure window, driver (name/photo/rating), walk
  distance to pickup, price suggestion.
- All prices labeled "προτεινόμενη συνεισφορά" (suggested contribution) — never
  "fare".

## Push handling (Codex-flagged reliability rules)

- Push is **best-effort and possibly duplicate**: every notification carries
  `idempotency_key` + entity ids; tapping opens the entity screen which fetches
  fresh state (never trust push payload as state).
- Token lifecycle: register on login + on token-change listener; server disables
  tokens on `DeviceNotRegistered` receipts.
- Ask notifications use time-sensitive/high-priority channels; include expiry so
  a stale tap shows "this ask expired" instead of a broken screen.

## Background location (post-MVP live ride — design constraints)

- Requires dev-build + store-review-sensitive permissions (iOS "Always" or
  in-use + live activity; Android foreground service with visible notification).
- Only ever active during an explicit "ride started" session — start/stop tied to
  user action, auto-stop at destination arrival or timeout. No passive tracking,
  ever (privacy + battery + store review).
- Expect delivery gaps (OEM task killers, force-quit). The server treats live
  position as advisory; trips never depend on it.
- WebSockets do not survive backgrounding — positions go over batched HTTP POST.

## Offline behavior

- Read-only cache of upcoming trips available offline (react-query persisted).
- Mutations (accept/decline/request) require connectivity; queued retries only
  for chat messages.

## Build/distribution

- EAS Build for both stores; internal distribution via TestFlight + APK direct
  for the pilot community.
- OTA updates via `expo-updates` for JS-only changes.

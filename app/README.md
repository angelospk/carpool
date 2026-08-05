# The Expo app

## Running it in a browser

Fastest way to look at the app without a phone. Verified working on 3 August
2026: bundles in about 7 seconds, 1.4 MB, serves and loads.

```sh
npm install --legacy-peer-deps react-dom react-native-web   # once
npx expo start --web                                        # dev server
npx expo export --platform web --output-dir /tmp/web        # static build
```

`--legacy-peer-deps` is needed because Expo 57's own peer graph does not resolve
cleanly on npm 10. It is not optional and it is not a sign anything is wrong
with these two packages in particular.

Point it at a backend with `EXPO_PUBLIC_API_URL`; without one it falls back to
`http://localhost:8080`.

Verified in a headless browser on 3 August 2026: the page loads with no console
errors and a full sign-in — phone, OTP, name, region — reaches the home screen.

**What the browser will not tell you.** Push notifications do not exist on web.
`expo-secure-store` has no web implementation, so tokens land somewhere less
protected than on a device — fine for looking around, not a security test. And
anything that depends on a native module behaves differently or not at all.

Use the browser for layout, copy, navigation and state. Use an APK for anything
you would actually trust.

## Testing from another machine on the same network

The build machine here is `192.168.0.154`. Substitute your own.

```sh
# 1. backend, on the build machine. DEV_MODE prints OTP codes to the log
#    instead of sending an SMS, and allows the browser to call the API.
DATABASE_URL=postgres://carpool:carpool@localhost:5433/lan?sslmode=disable \
JWT_SECRET=at-least-32-bytes-of-something \
DEV_MODE=true HTTP_ADDR=:8080 \
REGION_FIXTURE=$PWD/fixtures/regions/greece.geojson \
./bin/api

# 2. tell the app where that is, then build. The .env matters: an inline
#    EXPO_PUBLIC_API_URL is NOT picked up by `expo export`.
echo 'EXPO_PUBLIC_API_URL=http://192.168.0.154:8080' > app/.env
cd app && npx expo export --clear --platform web --output-dir /tmp/web

# 3. serve it on the network
cd /tmp/web && python3 -m http.server 8099 --bind 0.0.0.0
```

Then open `http://192.168.0.154:8099` from the laptop.

Two traps, both of which cost time here:

**`--clear` is not optional when the API URL changes.** Metro caches the bundle
and a rebuild that takes 456 ms instead of 7 s did not rebuild anything — it
reused the old bundle with the old URL baked in, silently.

**The API must be on `DEV_MODE`.** Without it there are no CORS headers and
`OPTIONS` returns 405, so the browser refuses every request before sending it.
Nothing appears in the API log, because nothing reached the API.

Verified end to end on 3 August 2026: sign-in over the LAN address returns an
access token.

## Node version

`package.json` wants Node `^22.13.0`; this machine has 22.11.0 and npm warns
about it on every install. It builds anyway. If something fails oddly during a
native build, check this first.

## Dependencies that are installed and never imported

Four of them, and they are worth a decision rather than a shrug, because in
React Native an unused dependency is not free — it is linked into the native
build and ships in the APK.

| Package | Why it is here | Cost of keeping it |
| --- | --- | --- |
| `react-native-maps` | a map view that was never built | pulls in Google Play Services Maps; also breaks the web build the moment it is imported |
| `expo-location` | ditto | permission prompts already declared in `app.json` |
| `expo-notifications` | the server has `PUT /v1/me/push-token`; the app never calls it | small |
| `expo-device` | only used alongside notifications | small |

There is no map in the app on purpose — see `src/design/routeGlyph.ts`, which
draws the real shape of a route as a vector mark instead. The route is
recognisable from its shape alone, there is no tile key to hold and no vendor to
pay, and a tile view of rural Thrace tells a rider nothing they do not know.

What is genuinely missing is not the map view but the **picker**: pickup points
are typed as `"25.87, 40.85 Πλατεία"` today, which no pilot user will do.
`src/design/pin.ts` parses that, and is written so a picker can replace the
input without changing the shape it produces.

Push registration is the other real gap. The endpoint exists, the app never
registers, so nothing is ever delivered.

## The map

The app draws no map until a tile source is configured. That is deliberate:
OpenStreetMap's own tile servers are for OSM's own site and their policy asks
applications not to use them, so shipping a default pointed at
`tile.openstreetmap.org` would be freeloading on a charity's bandwidth and would
be cut off. MapTiler, Stadia and Protomaps all have free tiers; each wants a
key, and a key belongs to whoever deploys this rather than to this repository.

One variable, an XYZ raster template:

```
EXPO_PUBLIC_MAP_TILES_URL=https://tiles.example/{z}/{x}/{y}.png?key=…
EXPO_PUBLIC_MAP_ATTRIBUTION=© MapTiler © OpenStreetMap contributors
```

Raster rather than vector because it is the one shape every provider offers and
the one both platforms draw with what is already installed: maplibre-gl in the
browser, react-native-maps' `UrlTile` on a phone. Without the variable both
render a short notice instead, and every screen works as it did before.

The attribution is required — by the provider's terms, and by ODbL for the
contributors underneath whichever provider.

A vector renderer (`@maplibre/maplibre-react-native`) would be the better answer
on a phone and is a larger decision: it is a native module, so it cannot run in
Expo Go and needs a development build. Worth doing when the map is more than a
picture with pins on it.

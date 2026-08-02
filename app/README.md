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

**What the browser will not tell you.** Push notifications do not exist on web.
`expo-secure-store` has no web implementation, so tokens land somewhere less
protected than on a device — fine for looking around, not a security test. And
anything that depends on a native module behaves differently or not at all.

Use the browser for layout, copy, navigation and state. Use an APK for anything
you would actually trust.

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

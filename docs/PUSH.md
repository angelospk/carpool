# Turning push notifications on (Android)

The code is ready: the app asks for a push token after sign-in, the server
sends through the Expo Push API, and CI puts the two missing pieces into the
APK when they exist. What is missing is two accounts only their owner can
create. About 10 minutes.

## 1. Expo project → `EAS_PROJECT_ID`

1. Sign in at https://expo.dev (create the account if there is none).
2. Projects → Create a project, slug `carpool`.
3. Copy the **Project ID** (a UUID) from the project page.
4. `gh secret set EAS_PROJECT_ID --body <the-uuid> -R angelospk/carpool`

## 2. Firebase → `GOOGLE_SERVICES_JSON_B64`

1. https://console.firebase.google.com → Add project → `carpool` (Analytics off).
2. Add app → Android → package name `gr.carpool.app` → Register.
3. Download `google-services.json`.
4. `base64 -w0 google-services.json | gh secret set GOOGLE_SERVICES_JSON_B64 -R angelospk/carpool`
5. For local builds, also copy it to `app/google-services.json` (git ignores it).

## 3. Let Expo send through Firebase

1. Firebase console → Project settings → Service accounts → Generate new private key.
2. expo.dev → the `carpool` project → Credentials → Android → `gr.carpool.app`
   → FCM V1 service account key → upload that JSON.

## 4. Build and check

1. `gh workflow run build-apk -R angelospk/carpool`, about 20 minutes.
2. Install from https://carpool.haroldpoi.dev/get, sign in, allow notifications.
3. The API log shows the token arriving (`PUT /v1/me/push-token`). A request on
   one of your routes from another account should then reach the phone.

Without these, every APK still builds: push is simply off, and the app logs
`push notifications are off: app.json has no extra.eas.projectId`.

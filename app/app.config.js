// app.json plus the two things push notifications need, each only when it is
// there, so that a build without them still comes out with push simply off.
//
//   EAS_PROJECT_ID        the Expo project the push token is issued against
//   GOOGLE_SERVICES_JSON  path to Firebase's google-services.json; defaults to
//                         ./google-services.json, which is not in git
//
// Both come from the person who owns the Expo and Firebase accounts; see
// docs/PUSH.md.
const fs = require('fs');
const path = require('path');

module.exports = ({ config }) => {
  const projectId = process.env.EAS_PROJECT_ID;
  const googleServices =
    process.env.GOOGLE_SERVICES_JSON ?? path.join(__dirname, 'google-services.json');

  // EXPO_WEB_BASE_URL serves the web build under a path rather than at the
  // root: the API image ships it at /app, next to the landing page at /.
  // Native builds ignore baseUrl.
  const webBaseUrl = process.env.EXPO_WEB_BASE_URL;

  return {
    ...config,
    experiments: {
      ...config.experiments,
      ...(webBaseUrl ? { baseUrl: webBaseUrl } : {}),
    },
    extra: {
      ...config.extra,
      ...(projectId ? { eas: { ...config.extra?.eas, projectId } } : {}),
    },
    android: {
      ...config.android,
      ...(fs.existsSync(googleServices) ? { googleServicesFile: googleServices } : {}),
    },
  };
};

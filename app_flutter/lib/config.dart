/// Where the app talks to. A build sets it with
/// `--dart-define=API_URL=http://192.168.1.10:8080` for a phone on the local
/// network; without one it uses the public server.
const String apiUrl = String.fromEnvironment(
  'API_URL',
  defaultValue: 'https://carpool.haroldpoi.dev',
);

/// The Expo project that issues this device's push token. Empty when the build
/// was given none, and then push notifications stay off while the rest of the
/// app works.
const String easProjectId = String.fromEnvironment('EAS_PROJECT_ID');

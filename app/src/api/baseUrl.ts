// Which server the app talks to.
//
// On a device this is configuration and nothing else can be inferred: the app
// is a binary with no address of its own, so it uses EXPO_PUBLIC_API_URL, then
// app.json, then localhost.
//
// On web it is not configuration, and treating it as configuration is what
// broke. A statically exported bundle carries whatever URL was set at export
// time. Export against the LAN and open the page over Tailscale, or over a
// hostname, or from a different subnet, and every request goes to an address
// the browser cannot route to. The page loads, the first action fails, and the
// only thing the user sees is the generic error, because a request that never
// reaches a server produces no status code and no server log.
//
// So on web the API is assumed to sit on the same host as the page it came
// from, which is true of every way this is served: one machine runs the API and
// the static files. Only the port is taken from configuration, since that is a
// deployment detail rather than a routing one. Set EXPO_PUBLIC_API_SAME_HOST to
// "false" to point a web build at a genuinely different host.

type Input = {
  platform: string;
  env: Record<string, string | undefined>;
  location?: Pick<Location, 'hostname' | 'protocol'>;
  extraApiUrl?: string;
};

const DEFAULT = 'http://localhost:8080';
const strip = (url: string) => url.replace(/\/+$/, '');

export function resolveBaseUrl({ platform, env, location, extraApiUrl }: Input): string {
  const configured = env.EXPO_PUBLIC_API_URL ?? extraApiUrl ?? DEFAULT;

  const sameHost = platform === 'web' && env.EXPO_PUBLIC_API_SAME_HOST !== 'false' && location;
  if (!sameHost) {
    return strip(configured);
  }

  // The port is the only thing worth keeping from the configured URL. Parsing
  // is guarded because a malformed value should not stop the app from starting.
  let port = '8080';
  try {
    const parsed = new URL(configured);
    if (parsed.port) {
      port = parsed.port;
    }
  } catch {
    // leave the default
  }

  return `${location.protocol}//${location.hostname}:${port}`;
}

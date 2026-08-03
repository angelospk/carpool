import { resolveBaseUrl } from '../baseUrl';

describe('resolveBaseUrl on web', () => {
  const web = (host: string, protocol = 'http:') =>
    ({ hostname: host, protocol }) as Location;

  it('serves the API from whatever host the page came from', () => {
    // The whole bug this exists for: the page is fetched over Tailscale but the
    // bundle carries a LAN address, so every request goes to an IP the client
    // cannot route to and the app shows a generic failure.
    expect(resolveBaseUrl({ platform: 'web', env: {}, location: web('100.123.255.50') })).toBe(
      'http://100.123.255.50:8080',
    );
    expect(resolveBaseUrl({ platform: 'web', env: {}, location: web('192.168.0.154') })).toBe(
      'http://192.168.0.154:8080',
    );
    expect(resolveBaseUrl({ platform: 'web', env: {}, location: web('localhost') })).toBe(
      'http://localhost:8080',
    );
  });

  it('takes the port from the configured URL but not its host', () => {
    // A host baked in at build time is the thing that breaks. A port is a
    // deployment detail worth keeping.
    expect(
      resolveBaseUrl({
        platform: 'web',
        env: { EXPO_PUBLIC_API_URL: 'http://192.168.0.154:9999' },
        location: web('100.123.255.50'),
      }),
    ).toBe('http://100.123.255.50:9999');
  });

  it('follows the page to https so the browser does not block the request', () => {
    expect(
      resolveBaseUrl({ platform: 'web', env: {}, location: web('carpool.example.gr', 'https:') }),
    ).toBe('https://carpool.example.gr:8080');
  });

  it('obeys an explicit override, which is the only way to reach another host', () => {
    expect(
      resolveBaseUrl({
        platform: 'web',
        env: { EXPO_PUBLIC_API_URL: 'https://api.example.gr', EXPO_PUBLIC_API_SAME_HOST: 'false' },
        location: web('100.123.255.50'),
      }),
    ).toBe('https://api.example.gr');
  });
});

describe('resolveBaseUrl on a device', () => {
  it('uses the configured URL, because there is no page to take a host from', () => {
    expect(
      resolveBaseUrl({
        platform: 'ios',
        env: { EXPO_PUBLIC_API_URL: 'http://192.168.0.154:8080' },
        extraApiUrl: 'https://api.example.gr',
      }),
    ).toBe('http://192.168.0.154:8080');
  });

  it('falls back to app.json, then to localhost', () => {
    expect(resolveBaseUrl({ platform: 'android', env: {}, extraApiUrl: 'https://api.example.gr' })).toBe(
      'https://api.example.gr',
    );
    expect(resolveBaseUrl({ platform: 'android', env: {} })).toBe('http://localhost:8080');
  });
});

describe('resolveBaseUrl trailing slashes', () => {
  it('strips them, so joining a path cannot produce a double slash', () => {
    expect(
      resolveBaseUrl({
        platform: 'android',
        env: { EXPO_PUBLIC_API_URL: 'https://api.example.gr/' },
      }),
    ).toBe('https://api.example.gr');
  });
});

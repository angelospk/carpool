import { ApiError, NetworkError, configure, request } from '../client';
import { clearTokens, getTokens, resetTokenCache, setTokens } from '../../state/tokens';

const json = (status: number, body: unknown) =>
  Promise.resolve(
    new Response(JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    }),
  );

const noContent = () => Promise.resolve(new Response(null, { status: 204 }));

beforeEach(async () => {
  configure('http://test.local');
  resetTokenCache();
  await clearTokens();
  jest.restoreAllMocks();
});

test('sends the access token as a bearer header', async () => {
  await setTokens({ accessToken: 'access-1', refreshToken: 'refresh-1' });
  const fetchMock = jest.fn((_url: string, init?: RequestInit) => json(200, { ok: true }));
  global.fetch = fetchMock as unknown as typeof fetch;

  await request('/v1/me');

  const headers = fetchMock.mock.calls[0][1]!.headers as Record<string, string>;
  expect(headers.Authorization).toBe('Bearer access-1');
});

test('omits the bearer header on anonymous requests', async () => {
  await setTokens({ accessToken: 'access-1', refreshToken: 'refresh-1' });
  const fetchMock = jest.fn((_url: string, init?: RequestInit) => json(200, {}));
  global.fetch = fetchMock as unknown as typeof fetch;

  await request('/v1/regions', { anonymous: true });

  const headers = fetchMock.mock.calls[0][1]!.headers as Record<string, string>;
  expect(headers.Authorization).toBeUndefined();
});

test('surfaces the server error envelope as an ApiError', async () => {
  global.fetch = jest.fn(() =>
    json(409, { error: { code: 'ask_expired', message: 'this request has expired' } }),
  ) as unknown as typeof fetch;

  await expect(request('/v1/asks/1/accept', { method: 'POST' })).rejects.toMatchObject({
    status: 409,
    code: 'ask_expired',
  });
});

test('reports a transport failure as a NetworkError, not an ApiError', async () => {
  global.fetch = jest.fn(() => Promise.reject(new TypeError('offline'))) as unknown as typeof fetch;

  const err = await request('/v1/me').catch((e) => e);

  expect(err).toBeInstanceOf(NetworkError);
  expect(err).not.toBeInstanceOf(ApiError);
});

test('refreshes once on a 401 and replays the request', async () => {
  await setTokens({ accessToken: 'stale', refreshToken: 'refresh-1' });

  const fetchMock = jest
    .fn<Promise<Response>, [string, RequestInit?]>()
    .mockImplementationOnce(() => json(401, { error: { code: 'unauthorized' } }))
    .mockImplementationOnce(() => json(200, { access_token: 'fresh', refresh_token: 'refresh-2' }))
    .mockImplementationOnce(() => json(200, { id: 'u1' }));
  global.fetch = fetchMock as unknown as typeof fetch;

  const me = await request<{ id: string }>('/v1/me');

  expect(me.id).toBe('u1');
  const replayHeaders = fetchMock.mock.calls[2][1]!.headers as Record<string, string>;
  expect(replayHeaders.Authorization).toBe('Bearer fresh');
});

// Several queries commonly fail together when the access token expires. Each
// firing its own refresh would spend the single-use refresh token more than
// once, and the rotation replay detection would then revoke the whole family —
// logging the user out for doing nothing wrong.
test('concurrent 401s share a single refresh', async () => {
  await setTokens({ accessToken: 'stale', refreshToken: 'refresh-1' });

  let refreshCalls = 0;
  global.fetch = jest.fn((url: string) => {
    if (String(url).endsWith('/v1/auth/refresh')) {
      refreshCalls += 1;
      return json(200, { access_token: 'fresh', refresh_token: 'refresh-2' });
    }
    return refreshCalls === 0 ? json(401, { error: { code: 'unauthorized' } }) : json(200, {});
  }) as unknown as typeof fetch;

  await Promise.all([request('/v1/me'), request('/v1/trips/upcoming'), request('/v1/regions')]);

  expect(refreshCalls).toBe(1);
});

test('a failed refresh clears the session rather than looping', async () => {
  await setTokens({ accessToken: 'stale', refreshToken: 'dead' });

  global.fetch = jest.fn((url: string) =>
    String(url).endsWith('/v1/auth/refresh')
      ? json(401, { error: { code: 'invalid_token' } })
      : json(401, { error: { code: 'unauthorized' } }),
  ) as unknown as typeof fetch;

  await expect(request('/v1/me')).rejects.toBeInstanceOf(ApiError);

  expect(await getTokens()).toBeNull();
});

test('a 204 resolves without a body', async () => {
  global.fetch = jest.fn(noContent) as unknown as typeof fetch;

  await expect(request('/v1/trips/1/confirm', { method: 'POST' })).resolves.toBeUndefined();
});

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

// --- regression tests for issues found in adversarial review ---

// A refresh token is the whole session. A 500 or a rate limit from the refresh
// endpoint says nothing about whether the token is still valid, so discarding
// it there signs the user out for a server hiccup.
test('a server error during refresh keeps the refresh token', async () => {
  await setTokens({ accessToken: 'stale', refreshToken: 'refresh-1' });

  global.fetch = jest.fn((url: string) =>
    String(url).endsWith('/v1/auth/refresh')
      ? json(503, { error: { code: 'unavailable' } })
      : json(401, { error: { code: 'unauthorized' } }),
  ) as unknown as typeof fetch;

  await expect(request('/v1/me')).rejects.toBeInstanceOf(ApiError);

  expect(await getTokens()).not.toBeNull();
});

test('a rate-limited refresh keeps the refresh token', async () => {
  await setTokens({ accessToken: 'stale', refreshToken: 'refresh-1' });

  global.fetch = jest.fn((url: string) =>
    String(url).endsWith('/v1/auth/refresh')
      ? json(429, { error: { code: 'rate_limited' } })
      : json(401, { error: { code: 'unauthorized' } }),
  ) as unknown as typeof fetch;

  await expect(request('/v1/me')).rejects.toBeInstanceOf(ApiError);

  expect(await getTokens()).not.toBeNull();
});

// Signing out while a refresh is in flight must not put credentials back on the
// device: the next launch would silently sign the user back in.
test('a refresh that lands after sign-out does not restore credentials', async () => {
  await setTokens({ accessToken: 'stale', refreshToken: 'refresh-1' });

  let releaseRefresh: () => void = () => {};
  const refreshGate = new Promise<void>((resolve) => {
    releaseRefresh = resolve;
  });

  global.fetch = jest.fn(async (url: string) => {
    if (String(url).endsWith('/v1/auth/refresh')) {
      await refreshGate;
      return (await json(200, { access_token: 'fresh', refresh_token: 'refresh-2' })) as Response;
    }
    return (await json(401, { error: { code: 'unauthorized' } })) as Response;
  }) as unknown as typeof fetch;

  const inFlight = request('/v1/me').catch(() => undefined);

  // The user signs out while the refresh is still open.
  await clearTokens();
  releaseRefresh();
  await inFlight;

  expect(await getTokens()).toBeNull();
});

// The new token is only persisted after the response arrives. If the app dies
// in between, the token on disk is already spent and the next launch looks like
// a replay — which the server answers by ending the session. The attempt id is
// what tells the server "this is my retry, not a thief": it is written to disk
// *before* the request and reused until a refresh completes.
test('persists a refresh attempt id before asking, and reuses it after a lost response', async () => {
  await setTokens({ accessToken: 'access-1', refreshToken: 'refresh-1' });

  // First attempt: the response never arrives.
  global.fetch = jest.fn((url: string) =>
    url.endsWith('/v1/auth/refresh')
      ? Promise.reject(new TypeError('offline'))
      : json(401, { error: { code: 'expired', message: '' } }),
  ) as unknown as typeof fetch;
  await request('/v1/me').catch(() => undefined);

  const stored = await getTokens();
  expect(stored?.refreshAttemptId).toEqual(expect.any(String));

  // Second attempt, after a restart: the same id must go out.
  let meCalls = 0;
  const fetchMock = jest.fn((url: string, _init?: RequestInit) => {
    if (url.endsWith('/v1/auth/refresh')) {
      return json(200, { access_token: 'access-2', refresh_token: 'refresh-2' });
    }
    meCalls += 1;
    return meCalls === 1 ? json(401, { error: { code: 'expired', message: '' } }) : json(200, { ok: true });
  });
  global.fetch = fetchMock as unknown as typeof fetch;
  await request('/v1/me');

  const refreshCall = fetchMock.mock.calls.find(([url]) => url.endsWith('/v1/auth/refresh'))!;
  const sent = JSON.parse(refreshCall[1]!.body as string);
  expect(sent.attempt_id).toBe(stored!.refreshAttemptId);

  // Once the rotation lands, the id is spent and must not be reused.
  expect((await getTokens())?.refreshAttemptId).toBeUndefined();
});

// The id has to survive uuid.Parse on the server. A fallback that is merely
// unique would be rejected with a 400 — and, being persisted for reuse, would
// wedge every later refresh with the same rejected value.
test('generates a valid uuid even without crypto.randomUUID', async () => {
  await setTokens({ accessToken: 'access-1', refreshToken: 'refresh-1' });
  const realCrypto = globalThis.crypto;
  Object.defineProperty(globalThis, 'crypto', { value: undefined, configurable: true });

  try {
    global.fetch = jest.fn((url: string) =>
      url.endsWith('/v1/auth/refresh')
        ? Promise.reject(new TypeError('offline'))
        : json(401, { error: { code: 'expired', message: '' } }),
    ) as unknown as typeof fetch;
    await request('/v1/me').catch(() => undefined);
  } finally {
    Object.defineProperty(globalThis, 'crypto', { value: realCrypto, configurable: true });
  }

  expect((await getTokens())?.refreshAttemptId).toMatch(
    /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
  );
});

// A rejected attempt id must not be kept, or it wedges every later refresh.
test('a 400 discards the attempt id instead of retrying it forever', async () => {
  await setTokens({ accessToken: 'access-1', refreshToken: 'refresh-1', refreshAttemptId: 'stale' });

  global.fetch = jest.fn((url: string) =>
    url.endsWith('/v1/auth/refresh')
      ? json(400, { error: { code: 'invalid_request', message: '' } })
      : json(401, { error: { code: 'expired', message: '' } }),
  ) as unknown as typeof fetch;
  await request('/v1/me').catch(() => undefined);

  const stored = await getTokens();
  expect(stored?.refreshToken).toBe('refresh-1');
  expect(stored?.refreshAttemptId).toBeUndefined();
});

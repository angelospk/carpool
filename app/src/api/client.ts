// The API client.
//
// Two rules shape this file. First, the server is the source of truth: every
// screen fetches, and push notifications only ever say "something changed, go
// look". Second, a 401 must not turn into a refresh storm — several queries
// failing together share one refresh.

import { clearTokens, getTokens, setTokens, tokenEpoch } from '../state/tokens';

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    /** Whatever else the error envelope carried, such as the dates a
        standing arrangement could not reserve. */
    readonly details?: Record<string, unknown>,
  ) {
    super(message);
    this.name = 'ApiError';
  }

  /** True when the server rejected the request rather than failing. */
  get isClientError() {
    return this.status >= 400 && this.status < 500;
  }
}

export class NetworkError extends Error {
  constructor(cause: unknown) {
    super('the server could not be reached');
    this.name = 'NetworkError';
    this.cause = cause;
  }
}

type Options = {
  method?: string;
  body?: unknown;
  /** Skip the Authorization header, for the auth endpoints themselves. */
  anonymous?: boolean;
  signal?: AbortSignal;
  /** Let the request outlive the page, for the one sent as a tab closes. */
  keepalive?: boolean;
  /**
   * Return the body as text instead of parsing it. For the one response that
   * is a file rather than JSON; an error is still read as the JSON envelope.
   */
  text?: boolean;
};

let baseUrl = '';

/** Points the client at a server. Configurable so dev builds can reach a LAN box. */
export function configure(url: string) {
  baseUrl = url.replace(/\/+$/, '');
}

export function currentBaseUrl() {
  return baseUrl;
}

// A single in-flight refresh, shared by every request that gets a 401.
let refreshing: Promise<boolean> | null = null;

/** Called when the server tells us the session is over, so the UI can follow. */
let onSessionEnded: (() => void) | null = null;

export function setSessionEndedHandler(fn: (() => void) | null) {
  onSessionEnded = fn;
}

/**
 * A fresh rotation attempt id.
 *
 * The server parses this as a uuid, so the fallback must be a well-formed v4 and
 * not merely unique — an id the server rejects would be stored for reuse and
 * would then wedge every later refresh with the same 400. Randomness quality is
 * secondary: the id is a request nonce that grants nothing without the refresh
 * token it accompanies.
 */
export function newAttemptId(): string {
  const c = globalThis.crypto;
  if (c && typeof c.randomUUID === 'function') return c.randomUUID();

  const bytes = new Uint8Array(16);
  if (c && typeof c.getRandomValues === 'function') {
    c.getRandomValues(bytes);
  } else {
    for (let i = 0; i < bytes.length; i += 1) bytes[i] = Math.floor(Math.random() * 256);
  }
  bytes[6] = (bytes[6] & 0x0f) | 0x40; // version 4
  bytes[8] = (bytes[8] & 0x3f) | 0x80; // variant 1
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

async function refreshTokens(): Promise<boolean> {
  // Captured before the round trip. If the user signs out or signs in as
  // someone else meanwhile, the write below is refused rather than resurrecting
  // a dead session or clobbering the new one.
  const startedAt = tokenEpoch();

  const tokens = await getTokens();
  if (!tokens?.refreshToken) return false;

  // Durable before the request, so a retry after a crash presents the same id.
  // An id already on disk belongs to an attempt that never completed; reusing it
  // is the whole point.
  const attemptId = tokens.refreshAttemptId ?? newAttemptId();
  if (!tokens.refreshAttemptId) {
    if (!(await setTokens({ ...tokens, refreshAttemptId: attemptId }, startedAt))) return false;
  }

  let res: Response;
  try {
    res = await fetch(`${baseUrl}/v1/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: tokens.refreshToken, attempt_id: attemptId }),
    });
  } catch {
    // A transport failure says nothing about the token. Keep it.
    return false;
  }

  // Only an explicit rejection means the token is spent or revoked. A 500, a
  // 503 or a rate limit says nothing about validity, and discarding the token
  // there would sign the user out over a server hiccup.
  if (res.status === 401 || res.status === 403) {
    await clearTokens();
    onSessionEnded?.();
    return false;
  }
  if (res.status === 400) {
    // The server would not take this attempt id. Keeping it would repeat the
    // same rejected request forever, so drop it and let the next refresh mint a
    // new one. The token itself is untouched: a 400 says nothing about it.
    await setTokens({ accessToken: tokens.accessToken, refreshToken: tokens.refreshToken }, startedAt);
    return false;
  }
  if (!res.ok) return false;

  let body: { access_token?: string; refresh_token?: string };
  try {
    body = await res.json();
  } catch {
    return false;
  }
  if (!body.access_token || !body.refresh_token) return false;

  return setTokens(
    { accessToken: body.access_token, refreshToken: body.refresh_token },
    startedAt,
  );
}

/** Ensures at most one refresh runs at a time, whatever else is in flight. */
/** Shared by every caller so a screen full of expired requests refreshes once. */
export function refreshOnce(): Promise<boolean> {
  if (!refreshing) {
    refreshing = refreshTokens().finally(() => {
      refreshing = null;
    });
  }
  return refreshing;
}

/** Sends raw bytes rather than JSON, for the one endpoint that takes an image. */
export async function requestRaw<T>(path: string, body: Blob): Promise<T> {
  const send = async (): Promise<Response> => {
    const tokens = await getTokens();
    try {
      return await fetch(`${baseUrl}${path}`, {
        method: 'PUT',
        headers: tokens?.accessToken ? { Authorization: `Bearer ${tokens.accessToken}` } : {},
        body,
      });
    } catch (cause) {
      throw new NetworkError(cause);
    }
  };

  let res = await send();
  // The same refresh-and-retry as request(). Without it, choosing a photo more
  // than fifteen minutes after the last request fails once for no reason a
  // person could act on. The Blob is re-readable, so the retry is safe.
  if (res.status === 401) {
    if (await refreshOnce()) {
      res = await send();
    }
  }

  if (!res.ok) {
    const envelope = await res.json().catch(() => null);
    const error = envelope?.error ?? {};
    throw new ApiError(
      res.status,
      error.code ?? 'unknown',
      error.message ?? 'the request failed',
      error.details,
    );
  }
  return (await res.json()) as T;
}

export async function request<T>(path: string, options: Options = {}): Promise<T> {
  const send = async (): Promise<Response> => {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    if (!options.anonymous) {
      const tokens = await getTokens();
      if (tokens?.accessToken) headers.Authorization = `Bearer ${tokens.accessToken}`;
    }

    try {
      return await fetch(`${baseUrl}${path}`, {
        method: options.method ?? 'GET',
        headers,
        body: options.body === undefined ? undefined : JSON.stringify(options.body),
        signal: options.signal,
        keepalive: options.keepalive,
      });
    } catch (cause) {
      throw new NetworkError(cause);
    }
  };

  let res = await send();

  if (res.status === 401 && !options.anonymous) {
    if (await refreshOnce()) {
      res = await send();
    }
  }

  if (res.status === 204) return undefined as T;

  const text = await res.text();
  if (res.ok && options.text) return text as T;
  const body = text ? safeParse(text) : undefined;

  if (!res.ok) {
    const err = body?.error;
    throw new ApiError(res.status, err?.code ?? 'unknown', err?.message ?? res.statusText, err);
  }
  return body as T;
}

function safeParse(text: string) {
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

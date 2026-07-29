// The API client.
//
// Two rules shape this file. First, the server is the source of truth: every
// screen fetches, and push notifications only ever say "something changed, go
// look". Second, a 401 must not turn into a refresh storm — several queries
// failing together share one refresh.

import { getTokens, setTokens, clearTokens } from '../state/tokens';

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
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

async function refreshTokens(): Promise<boolean> {
  const tokens = await getTokens();
  if (!tokens?.refreshToken) return false;

  try {
    const res = await fetch(`${baseUrl}/v1/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: tokens.refreshToken }),
    });
    if (!res.ok) {
      // The refresh token is spent or revoked; the session is over.
      await clearTokens();
      return false;
    }
    const body = await res.json();
    await setTokens({
      accessToken: body.access_token,
      refreshToken: body.refresh_token,
    });
    return true;
  } catch {
    // A network failure is not a dead session; leave the tokens alone.
    return false;
  }
}

/** Ensures at most one refresh runs at a time, whatever else is in flight. */
function refreshOnce(): Promise<boolean> {
  if (!refreshing) {
    refreshing = refreshTokens().finally(() => {
      refreshing = null;
    });
  }
  return refreshing;
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
  const body = text ? safeParse(text) : undefined;

  if (!res.ok) {
    const err = body?.error;
    throw new ApiError(res.status, err?.code ?? 'unknown', err?.message ?? res.statusText);
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

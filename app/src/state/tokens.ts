// Credential storage.
//
// Where the tokens actually sit is decided by ./secureStorage, which is the
// Keychain or the Keystore on a device and localStorage on web, because
// expo-secure-store has no web implementation and throwing on import took the
// whole web build down. That file explains what the web fallback costs.
//
// Every mutation carries an epoch. A refresh is a read-modify-write that spans a
// network round trip, so by the time it finishes the user may have signed out or
// signed in as someone else. Writing unconditionally would put credentials back
// on a device that just logged out, or overwrite one account's tokens with
// another's. The epoch makes those writes detectable and refusable.

import { storage } from './secureStorage';

const KEY = 'carpool.tokens';

export type Tokens = {
  accessToken: string;
  refreshToken: string;
  /**
   * Identifies the in-flight rotation of `refreshToken`, written to disk before
   * the request goes out. Presenting a spent token with the id that spent it
   * tells the server this is a retry of one request rather than two parties
   * holding one credential, so a lost response costs a round trip instead of
   * the whole session. Absent when no rotation is outstanding.
   */
  refreshAttemptId?: string;
};

let cached: Tokens | null | undefined;
let epoch = 0;

// All access is serialized through this chain. Without it a slow cold read can
// land after a clear and put the old tokens back into the cache.
let queue: Promise<unknown> = Promise.resolve();

function serialize<T>(fn: () => Promise<T>): Promise<T> {
  const run = queue.then(fn, fn);
  queue = run.catch(() => undefined);
  return run;
}

/** The current credential epoch. It changes on every sign-in and sign-out. */
export function tokenEpoch(): number {
  return epoch;
}

export function getTokens(): Promise<Tokens | null> {
  return serialize(async () => {
    if (cached !== undefined) return cached;
    const raw = await storage.getItemAsync(KEY);
    // Re-check: a clear may have run while the read was outstanding.
    if (cached !== undefined) return cached;
    cached = raw ? (JSON.parse(raw) as Tokens) : null;
    return cached;
  });
}

/**
 * Stores tokens, unless the epoch moved while the caller was working.
 *
 * Returns false when the write was refused, which tells a refresh that its
 * result is stale and must be dropped rather than applied.
 */
export function setTokens(tokens: Tokens, expectedEpoch?: number): Promise<boolean> {
  return serialize(async () => {
    if (expectedEpoch !== undefined && expectedEpoch !== epoch) return false;

    // Disk first: if the write fails, the cache must not claim a token that is
    // not durably stored, or a restart would replay the older one and the
    // server would revoke the whole family.
    await storage.setItemAsync(KEY, JSON.stringify(tokens));
    cached = tokens;
    return true;
  });
}

const cleared = new Set<() => void | Promise<void>>();

/**
 * Called whenever the credentials are cleared — a sign-out, or a session the
 * server rejected — which is the one thing that means this phone is no longer
 * signed in as anybody. Clearing waits for every listener to finish: inside a
 * background task, the task ends when the request that failed returns, and
 * clean-up still running then would be cut off. Returns the way to stop
 * listening.
 */
export function onTokensCleared(listener: () => void | Promise<void>): () => void {
  cleared.add(listener);
  return () => void cleared.delete(listener);
}

/** Clears credentials and advances the epoch, invalidating any in-flight write. */
export function clearTokens(): Promise<void> {
  return serialize(async () => {
    epoch += 1;
    cached = null;
    await storage.deleteItemAsync(KEY);
    // One failing listener must not stop the others, nor the clearing.
    await Promise.all([...cleared].map((l) => Promise.resolve().then(l).catch(() => undefined)));
  });
}

/** Advances the epoch without clearing, for a sign-in that replaces a session. */
export function bumpTokenEpoch(): void {
  epoch += 1;
}

/** Drops the in-memory copy. Tests use this to start from a clean slate. */
export function resetTokenCache(): void {
  cached = undefined;
  queue = Promise.resolve();
}

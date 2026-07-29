// Credential storage.
//
// Tokens live in the secure store (Keychain / Keystore), never in AsyncStorage:
// AsyncStorage is plain text on disk and is included in device backups.

import * as SecureStore from 'expo-secure-store';

const KEY = 'carpool.tokens';

export type Tokens = {
  accessToken: string;
  refreshToken: string;
};

// Read-through cache, so the common path does not hit the keychain on every
// request.
let cached: Tokens | null | undefined;

export async function getTokens(): Promise<Tokens | null> {
  if (cached !== undefined) return cached;
  const raw = await SecureStore.getItemAsync(KEY);
  cached = raw ? (JSON.parse(raw) as Tokens) : null;
  return cached;
}

export async function setTokens(tokens: Tokens): Promise<void> {
  cached = tokens;
  await SecureStore.setItemAsync(KEY, JSON.stringify(tokens));
}

export async function clearTokens(): Promise<void> {
  cached = null;
  await SecureStore.deleteItemAsync(KEY);
}

/** Drops the in-memory copy. Tests use this to start from a clean slate. */
export function resetTokenCache(): void {
  cached = undefined;
}

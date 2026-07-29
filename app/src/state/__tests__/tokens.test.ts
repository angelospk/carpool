import * as SecureStore from 'expo-secure-store';

import {
  bumpTokenEpoch,
  clearTokens,
  getTokens,
  resetTokenCache,
  setTokens,
  tokenEpoch,
} from '../tokens';

beforeEach(async () => {
  resetTokenCache();
  await clearTokens();
  jest.clearAllMocks();
});

// Tokens must not land in AsyncStorage: it is plain text on disk and is swept
// up by device backups.
test('tokens are written to the secure store', async () => {
  await setTokens({ accessToken: 'a', refreshToken: 'r' });

  expect(SecureStore.setItemAsync).toHaveBeenCalledWith(
    'carpool.tokens',
    JSON.stringify({ accessToken: 'a', refreshToken: 'r' }),
  );
});

test('tokens round-trip', async () => {
  await setTokens({ accessToken: 'a', refreshToken: 'r' });
  resetTokenCache();

  expect(await getTokens()).toEqual({ accessToken: 'a', refreshToken: 'r' });
});

test('signing out removes them from the device', async () => {
  await setTokens({ accessToken: 'a', refreshToken: 'r' });

  await clearTokens();

  expect(SecureStore.deleteItemAsync).toHaveBeenCalledWith('carpool.tokens');
  expect(await getTokens()).toBeNull();
});

test('repeated reads hit the keychain once', async () => {
  await setTokens({ accessToken: 'a', refreshToken: 'r' });
  resetTokenCache();

  await getTokens();
  await getTokens();
  await getTokens();

  expect(SecureStore.getItemAsync).toHaveBeenCalledTimes(1);
});

// --- regression tests for issues found in adversarial review ---

// A refresh spans a network round trip. If the user signs out meanwhile, the
// write that lands afterwards must be refused — otherwise the next launch
// silently signs them back in.
test('a write from before a sign-out is refused', async () => {
  await setTokens({ accessToken: 'a', refreshToken: 'r' });
  const before = tokenEpoch();

  await clearTokens();
  const applied = await setTokens({ accessToken: 'rotated', refreshToken: 'r2' }, before);

  expect(applied).toBe(false);
  expect(await getTokens()).toBeNull();
});

// The same guard stops one account's late refresh from overwriting another's
// credentials after a sign-out and sign-in.
test('a write from before a new sign-in is refused', async () => {
  await setTokens({ accessToken: 'a-access', refreshToken: 'a-refresh' });
  const accountA = tokenEpoch();

  bumpTokenEpoch();
  await setTokens({ accessToken: 'b-access', refreshToken: 'b-refresh' });

  const applied = await setTokens({ accessToken: 'a-rotated', refreshToken: 'a-r2' }, accountA);

  expect(applied).toBe(false);
  expect(await getTokens()).toEqual({ accessToken: 'b-access', refreshToken: 'b-refresh' });
});

test('a write with the current epoch is applied', async () => {
  await setTokens({ accessToken: 'a', refreshToken: 'r' });

  const applied = await setTokens({ accessToken: 'fresh', refreshToken: 'r2' }, tokenEpoch());

  expect(applied).toBe(true);
  expect(await getTokens()).toEqual({ accessToken: 'fresh', refreshToken: 'r2' });
});

// A failed keychain write must not leave the cache claiming a token that is not
// on disk: a restart would then replay the older one and the server would
// revoke the whole family.
test('a failed keychain write does not update the cache', async () => {
  await setTokens({ accessToken: 'a', refreshToken: 'r' });
  (SecureStore.setItemAsync as jest.Mock).mockRejectedValueOnce(new Error('keychain locked'));

  await expect(setTokens({ accessToken: 'never', refreshToken: 'never' })).rejects.toThrow();

  expect(await getTokens()).toEqual({ accessToken: 'a', refreshToken: 'r' });
});

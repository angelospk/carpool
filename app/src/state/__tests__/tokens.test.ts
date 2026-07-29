import * as SecureStore from 'expo-secure-store';

import { clearTokens, getTokens, resetTokenCache, setTokens } from '../tokens';

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

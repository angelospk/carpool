/* eslint-env jest */

// The secure store is native. Tests use an in-memory stand-in so credential
// handling can be asserted without a device keychain.
jest.mock('expo-secure-store', () => {
  const store = new Map();
  return {
    getItemAsync: jest.fn(async (k) => (store.has(k) ? store.get(k) : null)),
    setItemAsync: jest.fn(async (k, v) => void store.set(k, v)),
    deleteItemAsync: jest.fn(async (k) => void store.delete(k)),
    __store: store,
  };
});

jest.mock('expo-constants', () => ({
  __esModule: true,
  default: { expoConfig: { extra: { apiUrl: 'http://test.local' } } },
}));

// Silence the animation warning React Native emits in a jsdom environment.
global.__DEV__ = true;

// Screens render translated copy, so i18n has to be initialised or every label
// comes out as its key and assertions test nothing.
require('./src/i18n');

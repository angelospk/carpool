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

// Screens read the status bar inset (ui.tsx `Screen topInset`). The hook throws
// outside a provider rather than returning zeros, so without this every screen
// test fails on a value none of them assert.
jest.mock('react-native-safe-area-context', () =>
  require('react-native-safe-area-context/jest/mock').default,
);

// Ionicons loads its font asynchronously and re-renders when it arrives, which
// jest reports as an update outside act() in every screen that draws an icon.
// The stand-in renders the icon's name synchronously, so a test can still say
// which icon is where.
jest.mock('@expo/vector-icons/Ionicons', () => {
  const { Text } = require('react-native');
  const Ionicons = ({ name, ...rest }) => require('react').createElement(Text, { ...rest, name }, name);
  Ionicons.glyphMap = {};
  return { __esModule: true, default: Ionicons };
});

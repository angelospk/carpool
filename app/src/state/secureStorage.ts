// Where credentials are kept, per platform.
//
// On a device this is the Keychain or the Keystore, via expo-secure-store.
// AsyncStorage is deliberately not used there: it is plain text on disk and it
// is swept into device backups.
//
// On web there is no such thing. expo-secure-store has no web implementation at
// all — calling it throws `getValueWithKeyAsync is not a function`, which is
// what the web build did on load, before it had rendered anything a person
// could use. So web falls back to localStorage, which is readable by any script
// running on the origin.
//
// That fallback is honest only because the web build exists to look at layout
// and flows, not to hold anybody's real session. It is stated here rather than
// left to be discovered, and `isSecure` exists so a screen can say so out loud
// if it ever needs to.

import { Platform } from 'react-native';

export const isSecure = Platform.OS !== 'web';

type Store = {
  getItemAsync(key: string): Promise<string | null>;
  setItemAsync(key: string, value: string): Promise<void>;
  deleteItemAsync(key: string): Promise<void>;
};

function webStore(): Store {
  // Guarded because a web build can be rendered without a window — during a
  // static export, or in a test — and throwing there would break the render
  // rather than the storage.
  const ls = (): Storage | null => {
    try {
      return typeof window !== 'undefined' ? window.localStorage : null;
    } catch {
      return null; // storage disabled by the browser, e.g. blocked cookies
    }
  };
  return {
    async getItemAsync(key) {
      return ls()?.getItem(key) ?? null;
    },
    async setItemAsync(key, value) {
      ls()?.setItem(key, value);
    },
    async deleteItemAsync(key) {
      ls()?.removeItem(key);
    },
  };
}

function nativeStore(): Store {
  // Required lazily so that bundling for web never pulls the native module in.
  // A static import would put expo-secure-store in the web bundle, and it is
  // the import itself that fails there.
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const SecureStore = require('expo-secure-store') as Store;
  return SecureStore;
}

export const storage: Store = isSecure ? nativeStore() : webStore();

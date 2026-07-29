// Session state, kept deliberately small: who is signed in, and nothing else.
// Server data belongs to react-query, not here.

import { create } from 'zustand';

import { api } from '../api/endpoints';
import { clearTokens, getTokens, setTokens } from './tokens';
import type { Me } from '../api/endpoints';

type SessionState = {
  status: 'loading' | 'signedOut' | 'signedIn';
  me: Me | null;
  restore: () => Promise<void>;
  signIn: (tokens: { accessToken: string; refreshToken: string }) => Promise<void>;
  signOut: () => Promise<void>;
  setMe: (me: Me) => void;
};

export const useSession = create<SessionState>((set) => ({
  status: 'loading',
  me: null,

  // Called once at startup. A stored token is not proof of a live session, so
  // it is validated against /me before the app decides the user is signed in.
  restore: async () => {
    const tokens = await getTokens();
    if (!tokens) {
      set({ status: 'signedOut', me: null });
      return;
    }
    try {
      const me = await api.me();
      set({ status: 'signedIn', me });
    } catch {
      await clearTokens();
      set({ status: 'signedOut', me: null });
    }
  },

  signIn: async (tokens) => {
    await setTokens(tokens);
    const me = await api.me();
    set({ status: 'signedIn', me });
  },

  // Logout clears the credentials before anything else, so a failure partway
  // through cannot leave a signed-out UI with live tokens on disk.
  signOut: async () => {
    const tokens = await getTokens();
    await clearTokens();
    set({ status: 'signedOut', me: null });
    if (tokens?.refreshToken) {
      try {
        await api.logout(tokens.refreshToken);
      } catch {
        // The local session is already gone; the server token expires on its own.
      }
    }
  },

  setMe: (me) => set({ me }),
}));

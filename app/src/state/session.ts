// Session state, kept deliberately small: who is signed in, and nothing else.
// Server data belongs to react-query, not here.

import { create } from 'zustand';

import { api } from '../api/endpoints';
import { bumpTokenEpoch, clearTokens, getTokens, setTokens, tokenEpoch } from './tokens';
import { ApiError, setSessionEndedHandler } from '../api/client';
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
    const startedAt = tokenEpoch();

    const tokens = await getTokens();
    if (!tokens) {
      set({ status: 'signedOut', me: null });
      return;
    }

    try {
      const me = await api.me();
      // A sign-out or sign-in that happened while /me was outstanding wins;
      // applying this result would sign the user back in after they left.
      if (tokenEpoch() !== startedAt) return;
      set({ status: 'signedIn', me });
    } catch (e) {
      if (tokenEpoch() !== startedAt) return;

      // Only an authentication failure means the session is dead. Being offline
      // at launch, or a server having a bad minute, must not delete a perfectly
      // good refresh token and force the user to sign in again.
      const rejected = e instanceof ApiError && (e.status === 401 || e.status === 403);
      if (!rejected) {
        set({ status: 'signedOut', me: null });
        return;
      }
      await clearTokens();
      set({ status: 'signedOut', me: null });
    }
  },

  signIn: async (tokens) => {
    // A sign-in replaces whatever session was there, so any refresh still in
    // flight for the previous account is invalidated before the new tokens land.
    bumpTokenEpoch();
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

// When the server rejects a refresh, the UI has to follow the credentials out.
// Without this the app keeps showing authenticated screens while every request
// underneath it is anonymous.
setSessionEndedHandler(() => {
  useSession.setState({ status: 'signedOut', me: null });
});

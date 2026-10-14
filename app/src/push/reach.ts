import { create } from 'zustand';

/**
 * Whether a notification can reach this device right now: a push token has
 * been registered with the server on this launch. False until that happens,
 * and always false on the web, so the app never promises a notification it
 * cannot deliver.
 */
export const usePushReach = create<{ on: boolean; set: (on: boolean) => void }>((set) => ({
  on: false,
  set: (on) => set({ on }),
}));

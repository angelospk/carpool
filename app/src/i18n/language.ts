// The language somebody picked, kept across launches.
//
// Greek is the default and stays so for anybody who never touches this. The
// choice lives on the device, not the account: it is about who is holding this
// phone, and a server round trip before the first screen draws would be a
// flash of the wrong language on every start.

import i18n from 'i18next';

import { storage } from '../state/secureStorage';

const KEY = 'carpool.language';
export const LANGUAGES = ['el', 'en'] as const;
export type Language = (typeof LANGUAGES)[number];

export async function setLanguage(lng: Language): Promise<void> {
  await i18n.changeLanguage(lng);
  await storage.setItemAsync(KEY, lng).catch(() => undefined);
}

/** Applies a saved choice, if there is one. Called once at start. */
export async function restoreLanguage(): Promise<void> {
  const saved = await storage.getItemAsync(KEY).catch(() => null);
  if (saved && (LANGUAGES as readonly string[]).includes(saved) && saved !== i18n.language) {
    await i18n.changeLanguage(saved);
  }
}

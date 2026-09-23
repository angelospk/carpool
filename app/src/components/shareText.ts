// Hands a message to whatever the platform has for sending one.
//
// On a phone that is the share sheet. In a desktop browser there usually is
// none — navigator.share is missing, and react-native-web's Share rejects —
// so every "send" button in the app failed with "something went wrong" for
// anybody testing or administering from a laptop. There the message goes to
// the clipboard instead, and the caller says so.

import { Platform, Share } from 'react-native';

/** What happened: the sheet opened, or the text was copied. */
export type Shared = 'shared' | 'copied';

export async function shareText(message: string, title?: string): Promise<Shared> {
  if (Platform.OS === 'web' && typeof navigator !== 'undefined' && typeof navigator.share !== 'function') {
    await navigator.clipboard.writeText(message);
    return 'copied';
  }
  await Share.share(title ? { title, message } : { message });
  return 'shared';
}

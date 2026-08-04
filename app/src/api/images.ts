// Fetching an image that needs a session.
//
// Profile photos are behind the same authentication as everything else, and an
// <Image source={{uri}}> does not carry the Authorization header. On a device
// the header could be passed in the source; on web react-native-web drops it.
// Rather than have avatars work on one platform and silently not on the other,
// they are fetched here with the session and handed on as a data URL.
//
// The cache is what makes that affordable: a list of trips shows the same few
// faces over and over, and the address of a photo changes when the photo does,
// so an entry can never be stale.

import { currentBaseUrl } from './client';
import { getTokens } from '../state/tokens';

const cache = new Map<string, Promise<string | null>>();

// Bounded because this holds decoded images. Far more than a pilot's worth of
// distinct faces, and small enough that it cannot quietly become the largest
// thing in the process.
const MAX_ENTRIES = 60;

export function photoSource(path: string | null | undefined): Promise<string | null> {
  if (!path) return Promise.resolve(null);
  // Anything absolute is a legacy photo_url from before uploads existed, and
  // needs no session.
  if (!path.startsWith('/')) return Promise.resolve(path);

  const hit = cache.get(path);
  if (hit) return hit;

  const pending = load(path).catch(() => null);
  if (cache.size >= MAX_ENTRIES) {
    // Oldest first, which for avatars is close enough to least-used and costs
    // nothing to maintain.
    const oldest = cache.keys().next().value;
    if (oldest !== undefined) cache.delete(oldest);
  }
  cache.set(path, pending);
  return pending;
}

async function load(path: string): Promise<string | null> {
  const tokens = await getTokens();
  if (!tokens?.accessToken) return null;

  const res = await fetch(`${currentBaseUrl()}${path}`, {
    headers: { Authorization: `Bearer ${tokens.accessToken}` },
  });
  if (!res.ok) return null;

  const blob = await res.blob();
  return await new Promise<string | null>((resolve) => {
    const reader = new FileReader();
    reader.onerror = () => resolve(null);
    reader.onload = () => resolve(typeof reader.result === 'string' ? reader.result : null);
    reader.readAsDataURL(blob);
  });
}

/** Drops a photo from the cache, for when the person replaces their own. */
export function forgetPhoto(path: string | null | undefined) {
  if (path) cache.delete(path);
}

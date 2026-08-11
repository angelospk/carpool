// Registering this device to be told things.
//
// The server side of notifications has been complete for a while: it stores
// tokens, ages them out by last use, and queues a push for every ask, booking
// and now every trip that appears on somebody's corridor. None of it reached a
// phone, because nothing ever called the endpoint that records a token.
//
// The logic lives here rather than in the screen so it can be tested without a
// device. Everything that needs a real phone is injected.

/** Outcome of one attempt, for logging and for tests to assert on. */
export type Registration =
  | { done: false; why: 'not-a-device' | 'signed-out' | 'permission-denied' }
  | { done: false; why: 'failed'; error: unknown }
  | { done: true; token: string };

export type Platform = 'ios' | 'android';

export interface PushDeps {
  /** False on web and in a simulator, where there is no token to be had. */
  isDevice: boolean;
  platform: string;
  signedIn: boolean;
  getPermissions: () => Promise<{ granted: boolean; canAskAgain: boolean }>;
  requestPermissions: () => Promise<{ granted: boolean }>;
  getToken: () => Promise<{ data: string }>;
  send: (token: string, platform: Platform) => Promise<void>;
}

/**
 * register asks for permission if that is still possible, takes a token, and
 * tells the server about it.
 *
 * Run on every launch while signed in, not once at sign-up. Three separate
 * reasons, and any one of them is enough:
 *
 *   - A token is not permanent. It changes on reinstall, on restore to a new
 *     phone, and occasionally on its own.
 *   - The retention rule retires a token that has gone two years without a
 *     successful delivery. Somebody who reads everything in the app and never
 *     taps a notification looks abandoned to that rule. Re-registering on
 *     launch is what makes that a temporary loss rather than a permanent one,
 *     and it is the reason the sweep is safe to leave as it is.
 *   - Somebody who denied permission and later turned it on in Settings never
 *     tells the app. Looking again each launch is how that gets noticed.
 */
export async function register(deps: PushDeps): Promise<Registration> {
  if (!deps.signedIn) return { done: false, why: 'signed-out' };
  // Web has no Expo push token, and a simulator cannot be given one. Asking
  // anyway throws, and a thrown error on launch is a worse bug than no
  // notifications.
  if (!deps.isDevice || (deps.platform !== 'ios' && deps.platform !== 'android')) {
    return { done: false, why: 'not-a-device' };
  }

  try {
    let { granted, canAskAgain } = await deps.getPermissions();
    if (!granted && canAskAgain) {
      granted = (await deps.requestPermissions()).granted;
    }
    // Somebody who said no is not asked again. The system prompt is one-shot on
    // iOS, and re-asking through it is not possible anyway; what would be
    // possible is nagging with our own dialog, which is how an app earns being
    // muted rather than being read.
    if (!granted) return { done: false, why: 'permission-denied' };

    const token = await deps.getToken();
    await deps.send(token.data, deps.platform);
    return { done: true, token: token.data };
  } catch (error) {
    // Every step here can fail for reasons that have nothing to do with the
    // person using the app: the token is fetched from Expo's servers and needs
    // the network, and the credentials it needs are a build-time concern. None
    // of that is worth interrupting somebody who opened the app to find a lift.
    // The next launch tries again.
    return { done: false, why: 'failed', error };
  }
}

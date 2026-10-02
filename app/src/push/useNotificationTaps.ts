import { useEffect, useRef } from 'react';
import { Platform } from 'react-native';
import * as Notifications from 'expo-notifications';
import { useRouter } from 'expo-router';

import { destinationOf } from './destination';

/**
 * Opens what a tapped notification is about.
 *
 * Registration was wired and delivery worked, but a tap only launched the app
 * at its front page, where a driver's question was nowhere to be seen.
 *
 * A tap is handled once. The response is kept by the library until cleared, so
 * it is cleared as soon as it has been acted on — otherwise a remount would
 * open the same screen again. Until somebody is signed in it is left alone,
 * so a tap that launched the app is acted on once sign-in finishes.
 */
function useNativeNotificationTaps(signedIn: boolean) {
  const router = useRouter();
  const response = Notifications.useLastNotificationResponse();
  const handled = useRef<string | null>(null);

  useEffect(() => {
    if (!signedIn || !response) return;
    if (response.actionIdentifier !== Notifications.DEFAULT_ACTION_IDENTIFIER) return;
    const key = response.notification.request.identifier;
    if (handled.current === key) return;
    handled.current = key;
    const to = destinationOf(response.notification.request.content.data);
    Notifications.clearLastNotificationResponse();
    if (to) router.push(to as never);
  }, [signedIn, response, router]);
}

// A browser has no push here (registration skips it too), and the
// notifications module has nothing to report on web.
export const useNotificationTaps: (signedIn: boolean) => void =
  Platform.OS === 'web' ? () => {} : useNativeNotificationTaps;

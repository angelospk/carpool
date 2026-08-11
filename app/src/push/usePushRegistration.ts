import { useEffect } from 'react';
import { Platform } from 'react-native';
import * as Device from 'expo-device';
import * as Notifications from 'expo-notifications';

import { api } from '../api/endpoints';
import { register, type Platform as PushPlatform } from './register';

/**
 * usePushRegistration hands this device's push token to the server whenever
 * somebody is signed in.
 *
 * Everything the decision depends on lives in register(); this is the wiring
 * that gives it a real phone. Kept apart so the policy — when to ask, when to
 * stop asking, what to do when the network is down — can be tested without one.
 */
export function usePushRegistration(signedIn: boolean) {
  useEffect(() => {
    if (!signedIn) return;
    let cancelled = false;

    void register({
      isDevice: Device.isDevice,
      platform: Platform.OS,
      signedIn,
      getPermissions: async () => {
        const p = await Notifications.getPermissionsAsync();
        return { granted: p.granted, canAskAgain: p.canAskAgain };
      },
      requestPermissions: async () => {
        const p = await Notifications.requestPermissionsAsync();
        return { granted: p.granted };
      },
      getToken: () => Notifications.getExpoPushTokenAsync(),
      send: (token, platform) => api.registerPushToken(token, platform as PushPlatform),
    }).then((result) => {
      if (cancelled || result.done) return;
      // Logged rather than shown. Somebody who opened the app to find a lift
      // does not need to be told that a notification token could not be
      // fetched, and the next launch tries again.
      if (result.why !== 'failed') return;
      // One of these failures is not a failure at all, it is a missing piece of
      // configuration, and it looks like a mystery on a device unless it says
      // so: without an EAS project id there is nowhere to fetch a token from.
      // Named here rather than left as a coded error, because the person who
      // reads this log is the person who has to go and create the project.
      const code = (result.error as { code?: string } | undefined)?.code;
      if (code === 'ERR_NOTIFICATIONS_NO_EXPERIENCE_ID') {
        console.warn(
          'push notifications are off: app.json has no extra.eas.projectId, ' +
            'so there is no project to fetch a token for. Nothing else is wrong.',
        );
        return;
      }
      console.warn('push registration failed', result.error);
    });

    return () => {
      cancelled = true;
    };
  }, [signedIn]);
}

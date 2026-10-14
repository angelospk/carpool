import { useEffect } from 'react';
import { Platform } from 'react-native';
import * as Device from 'expo-device';
import * as Notifications from 'expo-notifications';
import Constants from 'expo-constants';

import { api } from '../api/endpoints';
import { register, type Platform as PushPlatform } from './register';
import { usePushReach } from './reach';

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
    // Off until this account, on this launch, has a token. Neither the last
    // account on the phone nor the last launch counts.
    usePushReach.getState().set(false);
    if (!signedIn) return;
    let cancelled = false;

    void register({
      isDevice: Device.isDevice,
      platform: Platform.OS,
      signedIn,
      // Both places Expo puts it: easConfig on a build made by EAS, and the
      // checked-in app.json otherwise.
      projectId:
        Constants.easConfig?.projectId ??
        (Constants.expoConfig?.extra?.eas as { projectId?: string } | undefined)?.projectId,
      getPermissions: async () => {
        const p = await Notifications.getPermissionsAsync();
        return { granted: p.granted, canAskAgain: p.canAskAgain };
      },
      requestPermissions: async () => {
        const p = await Notifications.requestPermissionsAsync();
        return { granted: p.granted };
      },
      getToken: (projectId) => Notifications.getExpoPushTokenAsync({ projectId }),
      send: (token, platform) => api.registerPushToken(token, platform as PushPlatform),
    }).then((result) => {
      if (cancelled) return;
      usePushReach.getState().set(result.done);
      if (result.done) return;
      // Logged rather than shown: somebody who opened the app to find a lift
      // does not need to be told that a notification token could not be
      // fetched, and the next launch tries again.
      //
      // The missing-configuration case is said plainly, because the person reading this log is the person who
      // has to go and create the EAS project. Everything else about the app
      // works without it.
      if (result.why === 'not-configured') {
        console.warn(
          'push notifications are off: app.json has no extra.eas.projectId, ' +
            'so there is no project to issue a token against. Nothing else is wrong.',
        );
        return;
      }
      if (result.why === 'failed') console.warn('push registration failed', result.error);
    });

    return () => {
      cancelled = true;
    };
  }, [signedIn]);
}

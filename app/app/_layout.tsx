import { useEffect } from 'react';
import { Stack, useRouter, useSegments } from 'expo-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Platform } from 'react-native';
import { useTranslation } from 'react-i18next';
import Constants from 'expo-constants';

import { configure } from '../src/api/client';
import { resolveBaseUrl } from '../src/api/baseUrl';
import { takeReturn } from '../src/state/returnTo';
import { useSession } from '../src/state/session';
import { usePushRegistration } from '../src/push/usePushRegistration';
import { useNotificationTaps } from '../src/push/useNotificationTaps';
import { registerCategories } from '../src/push/categories';
// Loaded at start for its side effect: the background task that receives the
// car's position during a drive has to exist before the OS hands it anything.
import { forgetDrive } from '../src/drive/tracking';
import { forgetOtherAccounts } from '../src/drive/detection';
import { restoreLanguage } from '../src/i18n/language';
import '../src/i18n';

// See src/api/baseUrl.ts. On a device this is configuration; on web the API is
// taken from the host that served the page, because a host baked in at export
// time is unreachable from anywhere but the network it was built for.
configure(
  resolveBaseUrl({
    platform: Platform.OS,
    env: {
      EXPO_PUBLIC_API_URL: process.env.EXPO_PUBLIC_API_URL,
      EXPO_PUBLIC_API_SAME_HOST: process.env.EXPO_PUBLIC_API_SAME_HOST,
    },
    location: typeof window !== 'undefined' ? window.location : undefined,
    extraApiUrl: Constants.expoConfig?.extra?.apiUrl as string | undefined,
  }),
);

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Reconcile on focus: push notifications only say "something changed",
      // so the app refetches rather than trusting a payload.
      refetchOnWindowFocus: true,
      staleTime: 30_000,
      retry: 2,
    },
  },
});

export default function RootLayout() {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthGate />
    </QueryClientProvider>
  );
}

// AuthGate keeps the URL and the session in step: a signed-out user is pushed
// into the auth flow, a signed-in one out of it.
function AuthGate() {
  const { t } = useTranslation();
  const status = useSession((s) => s.status);
  const restore = useSession((s) => s.restore);
  const segments = useSegments();
  const router = useRouter();

  useEffect(() => {
    void restore();
    void restoreLanguage();
  }, [restore]);

  // Every launch while signed in, not once at sign-up: a token changes on
  // reinstall and on restore to a new phone, the retention rule retires one
  // that has gone two years without a delivery, and somebody who turned
  // notifications on in Settings after refusing never tells the app.
  usePushRegistration(status === 'signedIn');
  // And a tap on one opens what it is about, not the front page.
  useNotificationTaps(status === 'signedIn');
  // What this phone does on its own for a driver — sharing a drive, noticing
  // a departure — belongs to the account that turned it on. Signed out, or
  // somebody else signed in, it stops: nobody inherits it.
  const account = useSession((s) => s.me?.id);
  useEffect(() => {
    if (status === 'loading') return;
    void forgetOtherAccounts(status === 'signedIn' ? account : undefined);
    if (status === 'signedOut') void forgetDrive();
  }, [status, account]);

  // The buttons an offer on a drive carries, in the language in use.
  useEffect(() => {
    void registerCategories(t);
  }, [t]);

  useEffect(() => {
    if (status === 'loading') return;

    const inAuthFlow = segments[0] === '(auth)';
    // A route link is how somebody meets the app before they have an account,
    // so it is the one screen a signed-out visitor is left on.
    const onPublicLink = segments[0] === 'r';
    if (status === 'signedOut' && !inAuthFlow && !onPublicLink) {
      router.replace('/(auth)/phone');
    } else if (status === 'signedIn' && inAuthFlow) {
      // Back to the route link that brought them here, if one did.
      router.replace((takeReturn() ?? '/(tabs)') as '/(tabs)');
    }
  }, [status, segments, router]);

  return (
    <Stack screenOptions={{ headerBackTitle: t('common.back') }}>
      <Stack.Screen name="(auth)" options={{ headerShown: false }} />
      <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
      {/* Without these, expo-router titles a screen after its own file, so the
          header above somebody's trip reads "trip/[id]". */}
      <Stack.Screen name="trip/[id]" options={{ title: t('trip.title') }} />
      <Stack.Screen name="ask/[id]" options={{ title: t('ask.title') }} />
      <Stack.Screen name="request/[id]" options={{ title: t('request.title') }} />
      <Stack.Screen name="user/[id]" options={{ title: t('user.title') }} />
      <Stack.Screen name="profile/phone" options={{ title: t('phone.title') }} />
      <Stack.Screen name="profile/routes" options={{ title: t('profile.routes') }} />
      <Stack.Screen name="profile/vehicles" options={{ title: t('garage.title') }} />
      <Stack.Screen name="profile/groups" options={{ title: t('groups.title') }} />
      <Stack.Screen name="profile/blocked" options={{ title: t('user.blockedTitle') }} />
      <Stack.Screen name="r/[token]" options={{ title: t('sharedRoute.title') }} />
    </Stack>
  );
}

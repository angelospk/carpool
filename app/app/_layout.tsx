import { useEffect } from 'react';
import { Stack, useRouter, useSegments } from 'expo-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Platform } from 'react-native';
import Constants from 'expo-constants';

import { configure } from '../src/api/client';
import { resolveBaseUrl } from '../src/api/baseUrl';
import { useSession } from '../src/state/session';
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
  const status = useSession((s) => s.status);
  const restore = useSession((s) => s.restore);
  const segments = useSegments();
  const router = useRouter();

  useEffect(() => {
    void restore();
  }, [restore]);

  useEffect(() => {
    if (status === 'loading') return;

    const inAuthFlow = segments[0] === '(auth)';
    if (status === 'signedOut' && !inAuthFlow) {
      router.replace('/(auth)/phone');
    } else if (status === 'signedIn' && inAuthFlow) {
      router.replace('/(tabs)');
    }
  }, [status, segments, router]);

  return (
    <Stack screenOptions={{ headerBackTitle: 'Πίσω' }}>
      <Stack.Screen name="(auth)" options={{ headerShown: false }} />
      <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
    </Stack>
  );
}

import { useEffect } from 'react';
import { Stack, useRouter, useSegments } from 'expo-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import Constants from 'expo-constants';

import { configure } from '../src/api/client';
import { useSession } from '../src/state/session';
import '../src/i18n';

// The server URL is configuration, not a constant: dev builds point at the mini
// PC on the LAN, production at the deployed host.
configure(
  (Constants.expoConfig?.extra?.apiUrl as string | undefined) ??
    process.env.EXPO_PUBLIC_API_URL ??
    'http://localhost:8080',
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

// A route link, opened in the app.
//
// Anybody can open it, signed in or not: it is how somebody who got the link
// in a chat meets the app for the first time. It shows what the link's page
// shows — no driver, no coordinates — and offers the next step: search this
// road if they have an account, make one if they do not.
//
// Searching resolves the two place names through the public places list, the
// same one the search form uses, rather than carrying coordinates in the link.

import { useState } from 'react';
import { StyleSheet, Text } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';

import { api, type SharedRoute } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { useSession } from '../../src/state/session';
import { Button, Card, Hint, RouteSpine, Screen, Title } from '../../src/components/ui';
import { shareRouteMessage } from '../../src/design/shareRoute';
import { colors, space, type } from '../../src/design/tokens';

export default function SharedRouteScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { token } = useLocalSearchParams<{ token: string }>();
  const signedIn = useSession((s) => s.status) === 'signedIn';
  const [error, setError] = useState<string | null>(null);

  const route = useQuery({
    queryKey: ['shared-route', token],
    queryFn: () => api.sharedRoute(token),
    retry: false,
  });

  const search = async (r: SharedRoute) => {
    setError(null);
    try {
      const [from, to] = await Promise.all([
        api.places(r.origin_label).then((p) => p.places[0]),
        api.places(r.destination_label).then((p) => p.places[0]),
      ]);
      if (!from || !to) {
        router.push('/(tabs)/search');
        return;
      }
      router.push({
        pathname: '/(tabs)/search',
        params: {
          preset: `link#${token}`,
          from_lon: String(from.lon),
          from_lat: String(from.lat),
          from_label: from.name,
          to_lon: String(to.lon),
          to_lat: String(to.lat),
          to_label: to.name,
        },
      });
    } catch {
      setError(t('common.error'));
    }
  };

  const gone = route.error instanceof ApiError && route.error.status === 410;

  return (
    <Screen>
      <Title>{t('sharedRoute.title')}</Title>
      {route.isLoading ? <Hint>{t('common.loading')}</Hint> : null}
      {route.isError ? (
        <Hint testID="shared-route-gone">{gone ? t('sharedRoute.retired') : t('sharedRoute.notFound')}</Hint>
      ) : null}
      {route.data ? (
        <Card testID="shared-route">
          <RouteSpine from={route.data.origin_label} to={route.data.destination_label} />
          {/* The same sentence the driver sent, so the screen and the chat
              agree about what was offered. Its first line only. */}
          <Text testID="shared-route-line" style={styles.line}>
            {shareRouteMessage(
              {
                ...route.data,
                oneoff_date: route.data.oneoff_date ?? undefined,
              },
              t,
            ).split('\n')[0]}
          </Text>
          {route.data.for_group ? <Hint>{t('sharedRoute.forGroup')}</Hint> : null}
          <Hint>{t('sharedRoute.howItWorks')}</Hint>
          {signedIn ? (
            <Button testID="shared-route-search" label={t('sharedRoute.search')} onPress={() => void search(route.data)} />
          ) : (
            <Button
              testID="shared-route-join"
              label={t('sharedRoute.join')}
              onPress={() => router.push('/(auth)/phone')}
            />
          )}
        </Card>
      ) : null}
      {error ? <Hint>{error}</Hint> : null}
    </Screen>
  );
}

const styles = StyleSheet.create({
  line: { ...type.body, color: colors.ink, marginTop: space.sm },
});

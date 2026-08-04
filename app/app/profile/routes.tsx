// What a driver has published, and how they stop publishing it.
//
// Retiring stops the route being offered from now on. It deliberately does not
// cancel departures already arranged: calling off tomorrow morning, when
// somebody is expecting a seat in it, is a different act from ceasing to offer
// a route, and doing the first silently because the driver asked for the second
// is how a passenger finds out by standing at a corner. So the count of
// upcoming trips is shown next to the button, and those are cancelled one at a
// time from the trip itself.

import { Alert, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, type MyRoute } from '../../src/api/endpoints';
import { Button, Card, Hint, Screen, SectionHeader, Title } from '../../src/components/ui';
import { colors, space, type } from '../../src/design/tokens';

const DAYS = ['', 'Δε', 'Τρ', 'Τε', 'Πε', 'Πα', 'Σα', 'Κυ'];

export default function MyRoutesScreen() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const routes = useQuery({
    queryKey: ['my-routes'],
    queryFn: () => api.myRoutes(),
  });

  const retire = useMutation({
    mutationFn: (id: string) => api.retireRoute(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['my-routes'] }),
  });

  const confirmRetire = (route: MyRoute) => {
    const ask =
      route.upcoming_trips > 0
        ? t('profile.retireWithTrips', { count: route.upcoming_trips })
        : t('profile.retireConfirm');
    Alert.alert(t('profile.retire'), ask, [
      { text: t('common.cancel'), style: 'cancel' },
      { text: t('profile.retire'), style: 'destructive', onPress: () => retire.mutate(route.id) },
    ]);
  };

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.body} showsVerticalScrollIndicator={false}>
        <Title>{t('profile.routes')}</Title>

        {routes.isLoading ? <Hint>{t('common.loading')}</Hint> : null}
        {routes.data?.routes.length === 0 ? <Hint>{t('profile.noRoutes')}</Hint> : null}

        {routes.data?.routes.map((r) => (
          <Card key={r.id} testID={`route-${r.id}`}>
            {/* No route glyph here, deliberately: it is drawn from the road
                geometry, and sending a polyline per route to a list that only
                needs to be recognised by its ends is a lot of payload for a
                decoration. */}
            <View style={styles.head}>
              <View style={styles.where}>
                <Text style={styles.route}>
                  {r.origin_label} → {r.destination_label}
                </Text>
                <Text style={styles.meta}>
                  {r.depart_earliest}–{r.depart_latest} · {r.route_km.toFixed(1)} χλμ ·{' '}
                  {t('publish.seatsN', { count: r.seats })}
                </Text>
                <Text style={styles.meta}>
                  {r.kind === 'recurring'
                    ? r.weekdays.map((d) => DAYS[d]).join(' ')
                    : (r.oneoff_date ?? '')}
                </Text>
              </View>
            </View>

            {/* The number that makes retiring an informed decision rather than
                a guess about what else it touches. */}
            {r.upcoming_trips > 0 ? (
              <Text style={styles.upcoming}>
                {t('profile.upcomingTrips', { count: r.upcoming_trips })}
              </Text>
            ) : null}

            <Button
              testID={`retire-${r.id}`}
              label={t('profile.retire')}
              variant="danger"
              disabled={retire.isPending}
              onPress={() => confirmRetire(r)}
            />
          </Card>
        ))}

        <SectionHeader title="" />
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { gap: space.md, paddingBottom: 140 },
  head: { flexDirection: 'row', gap: space.md, alignItems: 'center', marginBottom: space.sm },
  where: { flex: 1, gap: 2 },
  route: { ...type.body, color: colors.ink },
  meta: { ...type.caption, color: colors.muted },
  upcoming: { ...type.caption, color: colors.muted, marginBottom: space.sm },
});

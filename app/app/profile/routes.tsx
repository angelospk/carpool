// What a driver has published, and how they stop publishing it.
//
// Retiring stops the route being offered from now on. It deliberately does not
// cancel departures already arranged: calling off tomorrow morning, when
// somebody is expecting a seat in it, is a different act from ceasing to offer
// a route, and doing the first silently because the driver asked for the second
// is how a passenger finds out by standing at a corner. So the count of
// upcoming trips is shown next to the button, and those are cancelled one at a
// time from the trip itself.

import { useState } from 'react';
import { ScrollView, StyleSheet, Switch, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, type MyRoute } from '../../src/api/endpoints';
import {
  Badge,
  Button,
  Card,
  Hint,
  RouteSpine,
  Screen,
  SectionHeader,
  Title,
} from '../../src/components/ui';
import { ShareRouteButton } from '../../src/components/ShareRouteButton';
import { confirm } from '../../src/components/confirm';
import { colors, space, type } from '../../src/design/tokens';
import { calendarDay } from '../../src/design/format';

export default function MyRoutesScreen() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const routes = useQuery({
    queryKey: ['my-routes'],
    queryFn: () => api.myRoutes(),
  });

  const [error, setError] = useState<string | null>(null);

  const retire = useMutation({
    mutationFn: (id: string) => api.retireRoute(id),
    onSuccess: () => {
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ['my-routes'] });
    },
    // Without this, a retire that failed looked exactly like one that worked
    // until the list redrew with the route still in it.
    onError: () => setError(t('common.error')),
  });

  // Being told, shortly before a departure on this route, that somebody on
  // the road is waiting. Off until the driver turns it on.
  const nudge = useMutation({
    mutationFn: (v: { id: string; on: boolean }) => api.setRouteNudge(v.id, v.on),
    onSuccess: () => {
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ['my-routes'] });
    },
    onError: () => setError(t('common.error')),
  });

  const confirmRetire = (route: MyRoute) => {
    const ask =
      route.upcoming_trips > 0
        ? t('profile.retireWithTrips', { count: route.upcoming_trips })
        : t('profile.retireConfirm');
    confirm({
      title: t('profile.retire'),
      message: ask,
      confirmLabel: t('profile.retire'),
      cancelLabel: t('common.cancel'),
      destructive: true,
      onConfirm: () => retire.mutate(route.id),
    });
  };

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.body} showsVerticalScrollIndicator={false}>
        <Title>{t('profile.routes')}</Title>

        {routes.isLoading ? <Hint>{t('common.loading')}</Hint> : null}
        {routes.isError ? (
          <View style={styles.failed}>
            <Hint>{t('common.error')}</Hint>
            <Button testID="routes-retry" label={t('common.retry')} onPress={() => void routes.refetch()} />
          </View>
        ) : null}
        {error ? <Hint>{error}</Hint> : null}
        {routes.data?.routes.length === 0 ? <Hint>{t('profile.noRoutes')}</Hint> : null}

        {routes.data?.routes.map((r) => (
          <Card key={r.id} testID={`route-${r.id}`}>
            {/* No route glyph here, deliberately: it is drawn from the road
                geometry, and sending a polyline per route to a list that only
                needs to be recognised by its ends is a lot of payload for a
                decoration. */}
            <View style={styles.head}>
              <View style={styles.when}>
                <Text style={styles.time}>{r.depart_earliest}</Text>
                <Text style={styles.meta}>–{r.depart_latest}</Text>
              </View>
              <View style={styles.where}>
                <RouteSpine from={r.origin_label} to={r.destination_label} />
              </View>
            </View>

            {/* The days as the same pills the publish form picks them with,
                read-only, so the route reads the way it was set. */}
            <View style={styles.days}>
              {r.kind === 'recurring' ? (
                r.weekdays.map((d) => <Badge key={d} label={t(`weekdaysShort.${d}`)} tone="primary" />)
              ) : r.oneoff_date ? (
                <Badge label={calendarDay(r.oneoff_date)} tone="primary" />
              ) : null}
            </View>

            <Text style={styles.meta}>
              {r.route_km.toFixed(1)} χλμ · {t('publish.seatsN', { count: r.seats })}
            </Text>

            <View style={styles.badges}>
              {/* The number that makes retiring an informed decision rather
                  than a guess about what else it touches. */}
              {r.upcoming_trips > 0 ? (
                <Badge label={t('profile.upcomingTrips', { count: r.upcoming_trips })} />
              ) : null}
              {/* Whether sharing reached anybody. Opens, not people: one
                  person opening it twice is two. Nothing until there is one,
                  because "0" beside a link just shared reads as a failure. */}
              {r.link_opens ? (
                <Badge
                  testID={`link-opens-${r.id}`}
                  label={t('profile.linkOpens', { count: r.link_opens })}
                  tone="accent"
                />
              ) : null}
            </View>

            <View style={styles.toggle}>
              <Text style={styles.toggleLabel}>{t('drive.nudge')}</Text>
              <Switch
                testID={`nudge-${r.id}`}
                value={!!r.nudge_when_leaving}
                disabled={nudge.isPending}
                onValueChange={(on) => nudge.mutate({ id: r.id, on })}
                trackColor={{ true: colors.primary, false: colors.line }}
              />
            </View>
            <Text style={styles.meta}>{t('drive.nudgeHint')}</Text>

            <ShareRouteButton route={r} testID={`share-${r.id}`} failedTestID={`share-failed-${r.id}`} />
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
  head: { flexDirection: 'row', gap: space.lg, alignItems: 'center', marginBottom: space.sm },
  when: { alignItems: 'flex-start' },
  time: { ...type.display, fontSize: 26, color: colors.ink },
  where: { flex: 1 },
  days: { flexDirection: 'row', flexWrap: 'wrap', gap: space.xs, marginBottom: space.sm },
  badges: { flexDirection: 'row', flexWrap: 'wrap', gap: space.xs, marginVertical: space.sm },
  meta: { ...type.caption, color: colors.muted },
  failed: { gap: space.sm },
  toggle: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: space.md },
  toggleLabel: { ...type.body, color: colors.ink, flex: 1 },
});

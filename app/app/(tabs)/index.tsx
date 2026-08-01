import { FlatList, StyleSheet, Text, View } from 'react-native';
import { useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';

import { api, type TripSummary } from '../../src/api/endpoints';
import { PastTrips } from '../../src/components/PastTrips';
import { useSession } from '../../src/state/session';
import {
  AsyncState,
  Avatar,
  Badge,
  Card,
  RouteSpine,
  Screen,
  SectionHeader,
  SeatPips,
} from '../../src/components/ui';
import { RouteGlyph, decodeRoute } from '../../src/components/RouteGlyph';
import { colors, space, type } from '../../src/design/tokens';
import { athensDay, athensTime, startsNewDay } from '../../src/design/format';

export default function HomeScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const me = useSession((s) => s.me);

  // Shares react-query's cache key with PastTrips, so asking here costs no
  // extra request. It is asked at all because an empty upcoming list must not
  // hide the history — someone with nothing booked is exactly who wants to
  // repeat a ride.
  const past = useQuery({ queryKey: ['trips', 'past'], queryFn: () => api.pastTrips() });

  const trips = useQuery({
    queryKey: ['trips', 'upcoming'],
    queryFn: () => api.upcomingTrips(),
  });

  const all = trips.data?.trips ?? [];
  const [next, ...rest] = all;

  return (
    <Screen>
      <View style={styles.header}>
        <View style={styles.greeting}>
          <Avatar name={me?.name ?? '·'} photoUrl={me?.photo_url} size={44} />
          <View>
            <Text style={styles.hello}>{t('home.greeting')}</Text>
            <Text style={styles.name}>{me?.name ?? ''}</Text>
          </View>
        </View>
      </View>

      <AsyncState
        loading={trips.isLoading}
        error={trips.error}
        onRetry={() => void trips.refetch()}
        empty={!all.length && !(past.data?.trips.length ?? 0)}
        emptyLabel={t('home.empty')}
      >
        <FlatList
          testID="trip-list"
          data={rest}
          keyExtractor={(item) => item.id}
          refreshing={trips.isRefetching}
          onRefresh={() => void trips.refetch()}
          contentContainerStyle={styles.list}
          showsVerticalScrollIndicator={false}
          ListHeaderComponent={
            next ? (
              <View style={styles.listHeader}>
                <NextTrip trip={next} onPress={() => router.push(`/trip/${next.id}`)} />
                {rest.length ? <SectionHeader title={t('home.upcoming')} /> : null}
              </View>
            ) : null
          }
          ListFooterComponent={<PastTrips />}
          renderItem={({ item, index }) => (
            <>
              {startsNewDay(item.depart_earliest, rest[index - 1]?.depart_earliest) ? (
                <Text style={styles.day}>{athensDay(item.depart_earliest)}</Text>
              ) : null}
              <TripCard trip={item} onPress={() => router.push(`/trip/${item.id}`)} />
            </>
          )}
        />
      </AsyncState>
    </Screen>
  );
}

/**
 * The next trip gets the whole width and the route glyph. Everything a rider
 * checks on the morning of — when, where from, whether it is still on — sits
 * above the fold without a tap.
 */
function NextTrip({ trip, onPress }: { trip: TripSummary; onPress: () => void }) {
  const { t } = useTranslation();
  // The summary carries no geometry, so the glyph comes from the detail the
  // trip screen would load anyway — react-query hands the same cache to that
  // screen when it opens, so this costs one request and saves one later.
  const detail = useQuery({ queryKey: ['trips', trip.id], queryFn: () => api.trip(trip.id) });

  return (
    <Card testID={`trip-${trip.id}`} onPress={onPress}>
      <View style={styles.nextTop}>
        <View style={styles.nextWhen}>
          <Text style={styles.time}>{athensTime(trip.depart_earliest)}</Text>
          <Text style={styles.date}>{athensDay(trip.depart_earliest)}</Text>
        </View>
        <Badge
          label={trip.role === 'driver' ? t('home.asDriver') : t('home.asPassenger')}
          tone={trip.role === 'driver' ? 'primary' : 'quiet'}
        />
      </View>

      <View style={styles.nextBody}>
        <RouteGlyph
          points={decodeRoute(detail.data?.route_polyline)}
          seed={`${trip.origin_label}${trip.destination_label}`}
          width={104}
          height={92}
          testID="route-glyph"
        />
        <View style={styles.nextRoute}>
          <RouteSpine from={trip.origin_label} to={trip.destination_label} />
        </View>
      </View>

      <View style={styles.footer}>
        <SeatPips free={trip.seats_free} total={trip.seats_free + 1} testID="seat-pips" />
        <Text style={styles.meta}>{trip.route_km.toFixed(1)} χλμ</Text>
        {trip.price_eur > 0 ? (
          <Text style={styles.price}>{trip.price_eur.toFixed(2)} €</Text>
        ) : null}
      </View>
    </Card>
  );
}

function TripCard({ trip, onPress }: { trip: TripSummary; onPress: () => void }) {
  const { t } = useTranslation();

  return (
    <Card testID={`trip-${trip.id}`} onPress={onPress}>
      <View style={styles.rowCard}>
        <View style={styles.rowWhen}>
          <Text style={styles.rowTime}>{athensTime(trip.depart_earliest)}</Text>
          <View style={styles.rowRail} />
        </View>
        <View style={styles.rowMain}>
          <Text style={styles.route} numberOfLines={1}>
            {trip.origin_label} → {trip.destination_label}
          </Text>
          <Text style={styles.meta}>
            {trip.route_km.toFixed(1)} χλμ
            {trip.price_eur > 0 ? ` · ${trip.price_eur.toFixed(2)} €` : ''}
          </Text>
        </View>
        <Badge
          label={trip.role === 'driver' ? t('home.asDriver') : t('home.asPassenger')}
          tone={trip.role === 'driver' ? 'primary' : 'quiet'}
        />
      </View>
    </Card>
  );
}

const styles = StyleSheet.create({
  header: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  greeting: { flexDirection: 'row', alignItems: 'center', gap: space.md },
  hello: { ...type.caption, color: colors.muted },
  name: { ...type.title, color: colors.ink },

  list: { gap: space.md, paddingBottom: 120, paddingTop: space.sm },
  listHeader: { gap: space.md },
  day: { ...type.caption, color: colors.muted, marginTop: space.sm, marginLeft: space.xs },

  nextTop: { flexDirection: 'row', alignItems: 'flex-start', justifyContent: 'space-between' },
  nextWhen: { gap: 2 },
  time: { ...type.display, fontSize: 34, color: colors.ink },
  date: { ...type.caption, color: colors.muted },
  nextBody: { flexDirection: 'row', gap: space.lg, alignItems: 'center' },
  nextRoute: { flex: 1 },

  footer: { flexDirection: 'row', alignItems: 'center', gap: space.md },
  meta: { ...type.caption, color: colors.muted },
  price: { ...type.label, color: colors.primary, marginLeft: 'auto' },

  rowCard: { flexDirection: 'row', alignItems: 'center', gap: space.md },
  rowWhen: { alignItems: 'center', gap: 6 },
  rowTime: { ...type.label, fontSize: 15, color: colors.ink },
  rowRail: { width: 2, height: 18, borderRadius: 1, backgroundColor: colors.line },
  rowMain: { flex: 1, gap: 2 },
  route: { ...type.body, color: colors.ink },
});

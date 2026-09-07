import { FlatList, StyleSheet, Text, View } from 'react-native';
import { useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';

import { api, type TripSummary } from '../../src/api/endpoints';
import { PastTrips } from '../../src/components/PastTrips';
import { OpenRequests, useOpenRequests } from '../../src/components/OpenRequests';
import { Suggestions } from '../../src/components/Suggestions';
import { useSession } from '../../src/state/session';
import {
  AsyncState,
  Avatar,
  Badge,
  Card,
  RouteSpine,
  Button,
  Screen,
  SectionHeader,
} from '../../src/components/ui';
import { RouteGlyph, decodeRoute } from '../../src/components/RouteGlyph';
import { colors, space, type } from '../../src/design/tokens';
import { athensDay, athensTime, startsNewDay } from '../../src/design/format';

export default function HomeScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const me = useSession((s) => s.me);
  // The tabs mount before the session is settled, so without this both queries
  // fire without a token and come back 401 on every cold start — two failed
  // requests, two console errors, and on a phone two round trips over mobile
  // data before the sign-in screen has even been drawn.
  const signedIn = useSession((s) => s.status === 'signedIn');

  // Shares react-query's cache key with PastTrips, so asking here costs no
  // extra request. It is asked at all because an empty upcoming list must not
  // hide the history — someone with nothing booked is exactly who wants to
  // repeat a ride.
  const past = useQuery({
    queryKey: ['trips', 'past'],
    queryFn: () => api.pastTrips(),
    enabled: signedIn,
  });

  const trips = useQuery({
    queryKey: ['trips', 'upcoming'],
    queryFn: () => api.upcomingTrips(),
    enabled: signedIn,
  });

  // Asked here as well as inside the card so that the empty state can account
  // for it: somebody whose only activity is an open request must not be told
  // they have nothing, with the request they are waiting on hidden underneath.
  // The same hook as the card, so it is the same one query either way — and the
  // signed-in guard and the poll interval live in one place rather than two.
  const requests = useOpenRequests();

  const all = trips.data?.trips ?? [];
  const [next, ...rest] = all;

  return (
    <Screen topInset>
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
        // Everything has to be both settled and empty. A request still loading
        // is not an absence, and neither is one that failed to load.
        empty={
          !all.length &&
          !(past.data?.trips.length ?? 0) &&
          !(requests.data?.requests.length ?? 0) &&
          // Neither still asking nor having failed to ask. A query that broke
          // knows nothing about whether there is anything there, and "δεν έχεις
          // επόμενες διαδρομές" is a claim rather than a shrug.
          !requests.isLoading &&
          !requests.isError &&
          !past.isError
        }
        emptySlot={<FirstRun onAsk={() => router.push('/search')} onOffer={() => router.push('/publish')} />}
      >
        <FlatList
          testID="trip-list"
          data={rest}
          keyExtractor={(item) => item.id}
          refreshing={trips.isRefetching || requests.isRefetching}
          // Nothing here changes because of anything done on this phone: a
          // driver answers somewhere else and the screen finds out by asking.
          // So a pull refreshes the requests as well as the trips.
          onRefresh={() => {
            void trips.refetch();
            void requests.refetch();
          }}
          contentContainerStyle={styles.list}
          showsVerticalScrollIndicator={false}
          ListHeaderComponent={
            <View style={styles.listHeader}>
              {next ? <NextTrip trip={next} onPress={() => router.push(`/trip/${next.id}`)} /> : null}
              <OpenRequests />
              {rest.length ? <SectionHeader title={t('home.upcoming')} /> : null}
            </View>
          }
          ListFooterComponent={
            <>
              <PastTrips />
              <Suggestions />
            </>
          }
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

      {/* The names first and the drawing second. A long Greek place name is
          what somebody recognises the journey by; the glyph is texture. */}
      <View style={styles.nextBody}>
        <View style={styles.nextRoute}>
          <RouteSpine from={trip.origin_label} to={trip.destination_label} />
        </View>
        <RouteGlyph
          points={decodeRoute(detail.data?.route_polyline)}
          seed={`${trip.origin_label}${trip.destination_label}`}
          width={72}
          height={64}
          testID="route-glyph"
        />
      </View>

      <View style={styles.footer}>
        {/* Said in words rather than in pips. The pips drew a total of
            `seats_free + 1`, which is not the car's capacity and never was —
            it made a five-seater with one seat left look like a two-seater. */}
        <Text testID="seats-free" style={styles.meta}>
          {trip.seats_free > 0
            ? t('home.seatsFree', { count: trip.seats_free })
            : t('home.seatsFull')}
        </Text>
        <Text style={styles.meta}>{trip.route_km.toFixed(1)} χλμ</Text>
        {trip.price_eur > 0 ? (
          <View style={styles.priceBox}>
            {/* Bare euros beside a journey reads as a fare. It is not one. */}
            <Text style={styles.priceLabel}>{t('home.contribution')}</Text>
            <Text style={styles.price}>{trip.price_eur.toFixed(2)} €</Text>
          </View>
        ) : null}
      </View>
    </Card>
  );
}

/**
 * What the home screen says before anything has happened.
 *
 * "Δεν έχεις επόμενες διαδρομές" is true and useless: it tells a new arrival
 * nothing about what this is or what to do, at the exact moment they have the
 * most questions and the least patience. In a region of this size the honest
 * thing is also the useful one — say what the app does, say that somebody may
 * not turn up straight away, and offer both sides of the deal, because whoever
 * is reading this is as likely to have a car as to need one.
 */
function FirstRun({ onAsk, onOffer }: { onAsk: () => void; onOffer: () => void }) {
  const { t } = useTranslation();
  return (
    <View testID="empty" style={styles.firstRun}>
      <Text style={styles.firstRunTitle}>{t('home.emptyTitle')}</Text>
      <Text style={styles.firstRunBody}>{t('home.emptyBody')}</Text>
      <View style={styles.firstRunActions}>
        <Button testID="empty-ask" label={t('home.emptyAskSeat')} onPress={onAsk} />
        <Button
          testID="empty-offer"
          label={t('home.emptyOfferSeat')}
          variant="secondary"
          onPress={onOffer}
        />
      </View>
    </View>
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
  priceBox: { marginLeft: 'auto', alignItems: 'flex-end' },
  priceLabel: { ...type.caption, color: colors.muted, fontSize: 11 },
  price: { ...type.label, color: colors.primary },

  firstRun: { flex: 1, justifyContent: 'center', gap: space.md, paddingBottom: 80 },
  firstRunTitle: { ...type.display, fontSize: 26, color: colors.ink },
  firstRunBody: { ...type.body, color: colors.muted },
  firstRunActions: { gap: space.sm, marginTop: space.sm },

  rowCard: { flexDirection: 'row', alignItems: 'center', gap: space.md },
  rowWhen: { alignItems: 'center', gap: 6 },
  rowTime: { ...type.label, fontSize: 15, color: colors.ink },
  rowRail: { width: 2, height: 18, borderRadius: 1, backgroundColor: colors.line },
  rowMain: { flex: 1, gap: 2 },
  route: { ...type.body, color: colors.ink },
});

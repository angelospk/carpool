import { Animated, FlatList, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';

import { api, type TripSummary } from '../../src/api/endpoints';
import { PastTrips } from '../../src/components/PastTrips';
import { OpenRequests, useOpenRequests } from '../../src/components/OpenRequests';
import { Suggestions } from '../../src/components/Suggestions';
import { PendingAsks } from '../../src/components/PendingAsks';
import { SeatOffers } from '../../src/components/SeatOffers';
import { WeekendPlans, useWeekendPlans } from '../../src/components/WeekendPlans';
import { DrivingPrompt } from '../../src/components/DrivingPrompt';
import { useSession } from '../../src/state/session';
import {
  AsyncState,
  Avatar,
  Badge,
  Card,
  Button,
  usePressScale,
  Screen,
  SectionHeader,
  useBreakpoint,
} from '../../src/components/ui';
import { RouteGlyph, decodeRoute } from '../../src/components/RouteGlyph';
import { Ticket } from '../../src/components/Ticket';
import { Icon, type IconName } from '../../src/components/Icon';
import { border, colors, radius, size, space, type } from '../../src/design/tokens';
import { approxArrival } from '../../src/design/ticket';
import { athensDay, athensTime, eur, greetingKey, km, seatPrice, seatPriceNote, startsNewDay } from '../../src/design/format';
import { thisWeek } from '../../src/design/upcoming';

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
  // The same for a weekend plan: until the hourly job makes its requests, the
  // plan is all there is, and it must not be hidden behind «δεν έχεις».
  const plans = useWeekendPlans();
  // An offer is a question to answer, so it too keeps the screen from reading
  // as empty. The same key as the card, so the one query serves both.
  const offers = useQuery({ queryKey: ['offers'], queryFn: () => api.offers(), enabled: signedIn });

  const all = trips.data?.trips ?? [];
  const [next, ...upcoming] = all;
  // A recurring route is materialised weeks ahead; the list keeps the coming
  // week and counts the rest.
  const { shown: rest, later } = thisWeek(upcoming, new Date());

  // Everything has to be both settled and empty. A request still loading
  // is not an absence, and neither is one that failed to load.
  const empty =
    !all.length &&
    !(past.data?.trips.length ?? 0) &&
    !(requests.data?.requests.length ?? 0) &&
    !(plans.data?.plans.length ?? 0) &&
    !(offers.data?.offers.length ?? 0) &&
    !plans.isLoading &&
    // Neither still asking nor having failed to ask. A query that broke
    // knows nothing about whether there is anything there, and "δεν έχεις
    // επόμενες διαδρομές" is a claim rather than a shrug.
    !requests.isLoading &&
    !requests.isError &&
    !past.isError;

  const wide = useBreakpoint() === 'wide';

  const list = (
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
          {/* A seat a driver offered is a question too, and it lapses. Inside
              the list, so several of them scroll away instead of pushing the
              trips off the screen. */}
          <SeatOffers />
          {next ? <NextTrip trip={next} onPress={() => router.push(`/trip/${next.id}`)} /> : null}
          {wide ? null : (
            <>
              <OpenRequests />
              <WeekendPlans />
            </>
          )}
          {rest.length ? <SectionHeader title={t('home.upcoming')} /> : null}
        </View>
      }
      ListFooterComponent={
        <>
          {later > 0 ? (
            <Text testID="later-count" style={styles.day}>
              {t('home.laterCount', { count: later })}
            </Text>
          ) : null}
          {wide ? null : (
            <>
              <PastTrips />
              <Suggestions />
            </>
          )}
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
  );

  return (
    <Screen topInset column="wide">
      <Hero
        name={me?.name ?? ''}
        photoUrl={me?.photo_url}
        onAsk={() => router.push('/search')}
        onOffer={() => router.push('/publish')}
        quick={!(empty && !trips.isLoading && !trips.error)}
      />

      {/* A question waiting on this driver comes before anything else here:
          it lapses, and everything below it will still be there later. */}
      <PendingAsks />
      {/* The offers live in the list, which is not drawn while the trips load
          or after they failed. An offer lapses either way, so it stays here. */}
      {trips.isLoading || trips.error ? (
        // Bounded and scrolling, so several offers cannot push the error and
        // its retry off the screen.
        <ScrollView testID="offers-fallback" style={styles.offersFallback}>
          <SeatOffers />
        </ScrollView>
      ) : null}
      <DrivingPrompt trips={all} />

      <AsyncState
        loading={trips.isLoading}
        error={trips.error}
        onRetry={() => void trips.refetch()}
        empty={empty}
        emptySlot={
          <FirstRun
            onAsk={() => router.push('/search')}
            onOffer={() => router.push('/publish')}
            onWeekend={() => router.push('/weekend/new')}
          />
        }
      >
        {wide ? (
          // A laptop has the room for what a phone scrolls past: the trips on
          // the left, everything waiting or worth repeating beside them.
          <View style={styles.columns}>
            <View style={styles.main}>{list}</View>
            <ScrollView
              style={styles.side}
              contentContainerStyle={styles.sideBody}
              showsVerticalScrollIndicator={false}
            >
              <AtAGlance trips={all} />
              <OpenRequests />
              <WeekendPlans />
              <PastTrips />
              <Suggestions />
            </ScrollView>
          </View>
        ) : (
          list
        )}
      </AsyncState>
    </Screen>
  );
}

/**
 * The top of the home screen: who you are, what time of day it is, and the two
 * things this app is for, one tap away from wherever the list has scrolled to.
 */
function Hero({
  name,
  photoUrl,
  onAsk,
  onOffer,
  quick,
}: {
  name: string;
  photoUrl?: string | null;
  onAsk: () => void;
  onOffer: () => void;
  quick: boolean;
}) {
  const { t } = useTranslation();
  return (
    <View style={styles.hero}>
      <View style={styles.greeting}>
        <Avatar name={name || '·'} photoUrl={photoUrl} size={48} />
        <View style={styles.greetingText}>
          <Text style={styles.hello}>{t(greetingKey(new Date()))}</Text>
          <Text style={styles.name} numberOfLines={1}>
            {name}
          </Text>
        </View>
      </View>
      {quick ? (
        <View style={styles.quick}>
          <QuickAction testID="quick-ask" icon="search" label={t('home.quickAsk')} onPress={onAsk} />
          <QuickAction
            testID="quick-offer"
            icon="add"
            label={t('home.quickOffer')}
            onPress={onOffer}
            tone="accent"
          />
        </View>
      ) : null}
    </View>
  );
}

/**
 * The side column's first card: the week ahead in three numbers. It is there so
 * that a driver with nothing waiting and nothing to repeat still has something
 * beside the list, and it is the thing a regular driver wants to know anyway.
 */
function AtAGlance({ trips }: { trips: TripSummary[] }) {
  const { t } = useTranslation();
  const driving = trips.filter((x) => x.role === 'driver');
  const km = trips.reduce((sum, x) => sum + x.route_km, 0);
  const seats = driving.reduce((sum, x) => sum + x.seats_free, 0);
  return (
    <Card testID="at-a-glance">
      <Text style={styles.glanceTitle}>{t('home.sideTitle')}</Text>
      <View style={styles.glanceRow}>
        <Stat value={String(trips.length)} label={t('home.statTrips')} />
        <Stat value={`${Math.round(km)}`} label={t('home.statKm')} />
        {driving.length ? (
          <Stat value={String(seats)} label={t('home.statSeats')} tone="accent" />
        ) : null}
      </View>
    </Card>
  );
}

function Stat({ value, label, tone }: { value: string; label: string; tone?: 'accent' }) {
  return (
    <View style={styles.stat}>
      <Text style={[styles.statValue, tone === 'accent' && styles.statValueAccent]}>{value}</Text>
      <Text style={styles.statLabel}>{label}</Text>
    </View>
  );
}

function QuickAction({
  icon,
  label,
  onPress,
  tone = 'primary',
  testID,
}: {
  icon: IconName;
  label: string;
  onPress: () => void;
  tone?: 'primary' | 'accent';
  testID?: string;
}) {
  const press = usePressScale();
  return (
    <Pressable
      testID={testID}
      accessibilityRole="button"
      accessibilityLabel={label}
      onPress={onPress}
      onPressIn={press.onPressIn}
      onPressOut={press.onPressOut}
      style={styles.quickPress}
    >
      <Animated.View style={[styles.quickAction, tone === 'accent' && styles.quickActionAccent, press.style]}>
        <Icon name={icon} size={20} color={tone === 'accent' ? colors.ink : colors.paper} />
        <Text numberOfLines={2} style={[styles.quickLabel, tone === 'accent' && styles.quickLabelAccent]}>
          {label}
        </Text>
      </Animated.View>
    </Pressable>
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
    <Ticket
      testID={`trip-${trip.id}`}
      onPress={onPress}
      header={
        <>
          <Text style={styles.date}>{athensDay(trip.depart_earliest)}</Text>
          <Badge
            label={trip.role === 'driver' ? t('home.asDriver') : t('home.asPassenger')}
            tone={trip.role === 'driver' ? 'primary' : 'quiet'}
          />
        </>
      }
      depart={{ time: athensTime(trip.depart_earliest), place: trip.origin_label }}
      // The summary has no driving time, so the arrival is worked out from the
      // distance and says so with «≈».
      arrive={{
        time: athensTime(approxArrival(trip.depart_earliest, trip.route_km)),
        place: trip.destination_label,
        approx: true,
      }}
      distance={km(trip.route_km)}
      // The drawing is texture; the place names are what somebody recognises
      // the journey by, so it sits small beside them.
      aside={
        <RouteGlyph
          points={decodeRoute(detail.data?.route_polyline)}
          seed={`${trip.origin_label}${trip.destination_label}`}
          width={72}
          height={44}
          testID="route-glyph"
        />
      }
      // A passenger's trip is somebody else's car: who is driving is what
      // they check, not how many seats the driver has left.
      person={
        trip.role === 'passenger' && detail.data
          ? {
              name: detail.data.driver.name,
              label: t('home.withDriver', { name: detail.data.driver.name }),
              // Initials only: the photo would be one more request on the
              // home screen, and the trip page shows it.
              line: [detail.data.driver.car_model, detail.data.driver.car_color].filter(Boolean).join(' · '),
            }
          : undefined
      }
      lowerLeft={
        // Said in words rather than in pips. The pips drew a total of
        // `seats_free + 1`, which is not the car's capacity and never was —
        // it made a five-seater with one seat left look like a two-seater.
        trip.role === 'driver' ? (
          <Text testID="seats-free" style={styles.seats}>
            {trip.seats_free > 0 ? t('home.seatsFree', { count: trip.seats_free }) : t('home.seatsFull')}
          </Text>
        ) : undefined
      }
      // Bare euros beside a journey reads as a fare. It is not one.
      fare={trip.price_eur > 0 ? eur(trip.price_eur) : undefined}
      fareLabel={
        trip.price_eur > 0
          ? seatPriceNote(t, trip.price_eur, trip.seats) ?? t('home.contribution')
          : undefined
      }
    />
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
function FirstRun({
  onAsk,
  onOffer,
  onWeekend,
}: {
  onAsk: () => void;
  onOffer: () => void;
  onWeekend: () => void;
}) {
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
        <Button
          testID="empty-weekend"
          label={t('weekend.entry')}
          variant="secondary"
          onPress={onWeekend}
        />
      </View>
    </View>
  );
}

function TripCard({ trip, onPress }: { trip: TripSummary; onPress: () => void }) {
  const { t } = useTranslation();

  return (
    <Ticket
      testID={`trip-${trip.id}`}
      onPress={onPress}
      depart={{ time: athensTime(trip.depart_earliest), place: trip.origin_label }}
      arrive={{
        time: athensTime(approxArrival(trip.depart_earliest, trip.route_km)),
        place: trip.destination_label,
        approx: true,
      }}
      distance={km(trip.route_km)}
      note={
        <View style={styles.rowNote}>
          <Badge
            label={trip.role === 'driver' ? t('home.asDriver') : t('home.asPassenger')}
            tone={trip.role === 'driver' ? 'primary' : 'quiet'}
          />
          {trip.price_eur > 0 ? <Text style={styles.meta}>{seatPrice(t, trip.price_eur, trip.seats)}</Text> : null}
        </View>
      }
    />
  );
}

const styles = StyleSheet.create({
  offersFallback: { flexGrow: 0, maxHeight: '45%' },
  hero: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: space.md,
  },
  greeting: { flexDirection: 'row', alignItems: 'center', gap: space.md, flexShrink: 1 },
  greetingText: { flexShrink: 1 },
  hello: { ...type.caption, color: colors.muted },
  name: { ...type.display, fontSize: 28, lineHeight: 31, color: colors.ink },
  quick: { flexDirection: 'row', gap: space.sm, width: '100%' },
  quickPress: { flex: 1 },
  quickAction: {
    minHeight: size.tap + 4,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: space.sm,
    paddingHorizontal: space.md,
    paddingVertical: space.sm,
    borderRadius: radius.button,
    borderWidth: border.rest,
    borderColor: colors.ink,
    backgroundColor: colors.ink,
  },
  quickActionAccent: { backgroundColor: colors.paper },
  quickLabel: { ...type.button, fontSize: 15, color: colors.paper, flexShrink: 1 },
  quickLabelAccent: { color: colors.ink },

  columns: { flex: 1, flexDirection: 'row', gap: space.xl },
  main: { flex: 3, minWidth: 0 },
  side: { flex: 2 },
  sideBody: { gap: space.md, paddingTop: space.sm, paddingBottom: space.xl },
  glanceTitle: { ...type.caption, color: colors.muted, marginBottom: space.sm },
  glanceRow: { flexDirection: 'row', justifyContent: 'space-between', gap: space.md },
  stat: { flex: 1, gap: 2 },
  statValue: { ...type.time, fontSize: 30, lineHeight: 32, color: colors.ink },
  statValueAccent: { color: colors.ink },
  statLabel: { ...type.caption, color: colors.muted },

  list: { gap: space.md, paddingBottom: 120, paddingTop: space.sm },
  listHeader: { gap: space.md },
  day: { ...type.label, fontSize: 13, color: colors.muted, marginTop: space.sm },

  date: { ...type.label, color: colors.ink },
  seats: { ...type.label, color: colors.ink },
  meta: { ...type.caption, color: colors.muted },

  firstRun: { flex: 1, justifyContent: 'center', gap: space.md, paddingBottom: 80 },
  firstRunTitle: { ...type.display, color: colors.ink },
  firstRunBody: { ...type.body, color: colors.muted },
  firstRunActions: { gap: space.sm, marginTop: space.sm },

  rowNote: { flexDirection: 'row', alignItems: 'center', gap: space.md },
});

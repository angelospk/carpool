import { useEffect, useState, type ReactNode } from 'react';
import { FlatList, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, type Message, type TripDetail } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { useSession } from '../../src/state/session';
import {
  AsyncState,
  Avatar,
  Badge,
  Button,
  Card,
  RouteSpine,
  Screen,
  SeatPips,
  useBreakpoint,
} from '../../src/components/ui';
import { RouteGlyph, decodeRoute } from '../../src/components/RouteGlyph';
import { RepeatCard } from '../../src/components/RepeatCard';
import { RatingRow } from '../../src/components/RatingRow';
import { SocialLinks } from '../../src/components/SocialLink';
import { PortNotice } from '../../src/components/PortNotice';
import { PriceBreakdown } from '../../src/components/PriceBreakdown';
import { PreferenceChips } from '../../src/components/Preferences';
import { SharedGroups } from '../../src/components/SharedGroups';
import { LiveCard } from '../../src/components/LiveCard';
import { tripShareMessage } from '../../src/design/tripShare';
import { confirm as askFirst } from '../../src/components/confirm';
import { shareText } from '../../src/components/shareText';
import { Map } from '../../src/map/Map';
import polyline from '@mapbox/polyline';
import { haveTiles } from '../../src/map/tiles';
import { colors, radius, space, type } from '../../src/design/tokens';
import { athensDay, athensTime } from '../../src/design/format';

export default function TripScreen() {
  const { t } = useTranslation();
  const { id } = useLocalSearchParams<{ id: string }>();
  const queryClient = useQueryClient();
  const router = useRouter();
  const me = useSession((s) => s.me);
  const [draft, setDraft] = useState('');
  const [notice, setNotice] = useState<string | null>(null);

  const trip = useQuery({
    queryKey: ['trips', id],
    queryFn: () => api.trip(id),
    // After the trip, what can be done here waits on the other side: rating
    // opens once they confirm too. Polled until nothing is left to wait for,
    // so it appears without leaving and coming back.
    refetchInterval: (q) => (awaitingOthers(q.state.data) ? 30_000 : false),
  });
  const messages = useQuery({
    queryKey: ['trips', id, 'messages'],
    queryFn: () => api.messages(id),
    // Chat is polled while the screen is open. The websocket is the fast path;
    // this is the one that always works.
    refetchInterval: 10_000,
  });

  // A message that did not go says so, and stays in the box so sending it
  // again is one tap. It used to vanish without a word.
  const [sendFailed, setSendFailed] = useState(false);
  const send = useMutation({
    mutationFn: (body: string) => api.postMessage(id, body),
    onSuccess: () => {
      setDraft('');
      setSendFailed(false);
      void queryClient.invalidateQueries({ queryKey: ['trips', id, 'messages'] });
    },
    onError: () => setSendFailed(true),
  });
  const sendFailedLine = sendFailed ? (
    <Text testID="message-failed" style={styles.sendFailed}>
      {t('trip.sendFailed')}
    </Text>
  ) : null;

  const confirm = useMutation({
    mutationFn: () => api.confirmTrip(id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['trips', id] }),
    onError: (e) =>
      setNotice(
        e instanceof ApiError && e.code === 'not_departed'
          ? t('trip.notDeparted')
          : t('common.error'),
      ),
  });

  // Where the trip stands, by the device clock, which is the only one the
  // screen has; the server decides for real and answers if this is optimistic.
  //
  // Two edges, because the server uses two. Cancelling stops at the near end of
  // the departure window: from then on the car may be at the kerb. Confirming
  // starts at the far end: a trip that may leave any time until 07:30 has not
  // happened at 07:01. Between them there is nothing to do but go.
  //
  // Re-evaluated on a timer as well as on render, because somebody sitting on
  // this screen at 07:29 would otherwise still be offered cancel at 07:31.
  const [now, setNow] = useState(() => Date.now());
  const earliestAt = trip.data ? new Date(trip.data.depart_earliest).getTime() : null;
  const latestAt = trip.data ? new Date(trip.data.depart_latest).getTime() : null;
  const nextEdge = [earliestAt, latestAt].find((at) => at !== null && at > now) ?? null;
  useEffect(() => {
    if (nextEdge === null) return;
    const timer = setTimeout(() => setNow(Date.now()), nextEdge - now + 1000);
    return () => clearTimeout(timer);
  }, [nextEdge, now]);
  const phase: 'cancelled' | 'before' | 'underway' | 'after' =
    trip.data?.status === 'cancelled'
      ? 'cancelled'
      : earliestAt !== null && now < earliestAt
        ? 'before'
        : latestAt !== null && now < latestAt
          ? 'underway'
          : 'after';

  // Who the caller can still rate: the driver rates each passenger, a
  // passenger rates the driver. The server says who, per booking, because a
  // booking is only ratable once both sides have confirmed it.
  const toRate = trip.data
    ? trip.data.role === 'driver'
      ? trip.data.passengers.filter((p) => p.can_rate || p.rated)
      : [trip.data.driver].filter((p) => p.can_rate || p.rated)
    : [];

  // Somebody on the trip, pressable through to where they can be blocked or
  // reported. Never yourself.
  const openPerson = (p: { id: string; name: string }) =>
    router.push({ pathname: '/user/[id]', params: { id: p.id, name: p.name } });

  const cancel = useMutation({
    mutationFn: () => api.cancelTrip(id),
    onSuccess: () => {
      setNotice(t('trip.cancelled'));
      void trip.refetch();
      // The routes screen counts upcoming departures per route, and one of them
      // just stopped being upcoming.
      void queryClient.invalidateQueries({ queryKey: ['my-routes'] });
    },
    onError: (e) =>
      setNotice(
        e instanceof ApiError && e.code === 'departed' ? t('trip.notCancellable') : t('common.error'),
      ),
  });

  // A passenger giving back their seat. The server releases it and tells the
  // driver; the trip is then no longer theirs, and asking for it again is a
  // 404, so the screen leaves and forgets what it held.
  const leave = useMutation({
    mutationFn: (bookingId: string) => api.cancelBooking(bookingId),
    onSuccess: () => {
      queryClient.removeQueries({ queryKey: ['trips', id] });
      void queryClient.invalidateQueries({ queryKey: ['trips'] });
      void queryClient.invalidateQueries({ queryKey: ['requests'] });
      // Opened from a notification there is no screen behind this one.
      if (router.canGoBack()) router.back();
      else router.replace('/(tabs)');
    },
    onError: (e) => {
      setNotice(
        e instanceof ApiError && e.code === 'departed'
          ? t('trip.notCancellable')
          : e instanceof ApiError && (e.code === 'not_yours' || e.code === 'not_found')
            ? t('trip.leaveGone')
            : t('common.error'),
      );
      void trip.refetch();
    },
  });

  const wide = useBreakpoint() === 'wide';
  const renderMessage = ({ item }: { item: Message }) => {
              const mine = item.sender_id === me?.id;
              return (
                <View style={[styles.bubbleRow, mine && styles.bubbleRowMine]}>
                  <View style={[styles.bubble, mine && styles.bubbleMine]}>
                    {!mine ? <Text style={styles.sender}>{item.sender_name}</Text> : null}
                    <Text style={[styles.bubbleText, mine && styles.bubbleTextMine]}>
                      {item.body}
                    </Text>
                    <Text style={[styles.bubbleTime, mine && styles.bubbleTimeMine]}>
                      {athensTime(item.created_at)}
                    </Text>
                  </View>
                </View>
              );
            };
  const composerView = (
      <>
      {sendFailedLine}
      <View style={styles.composer}>
      <TextInput
        testID="message-input"
        style={styles.composerInput}
        value={draft}
        onChangeText={setDraft}
        placeholder={t('trip.messagePlaceholder')}
        placeholderTextColor={colors.muted}
      />
      <Button
        testID="message-send"
        label={t('trip.send')}
        onPress={() => send.mutate(draft)}
        disabled={!draft.trim() || send.isPending}
      />
    </View>
      </>
  );

  const details = trip.data ? (
              <View style={styles.head}>
                {/* The route's own shape, drawn from the geometry the server
                    routed — the pilot ships without map tiles. */}
                <Card>
                  <View style={styles.hero}>
                    <RouteGlyph
                      points={decodeRoute(trip.data.route_polyline)}
                      seed={`${trip.data.origin_label}${trip.data.destination_label}`}
                      width={120}
                      height={104}
                      testID="route-glyph"
                    />
                    <View style={styles.heroSide}>
                      <Text style={styles.time}>{athensTime(trip.data.depart_earliest)}</Text>
                      <Text style={styles.date}>{athensDay(trip.data.depart_earliest)}</Text>
                      <View style={styles.badges}>
                        <Badge
                          label={
                            trip.data.role === 'driver' ? t('home.asDriver') : t('home.asPassenger')
                          }
                          tone={trip.data.role === 'driver' ? 'primary' : 'quiet'}
                        />
                        <TripStatus phase={phase} confirmed={!!trip.data.confirmed_by_me} />
                      </View>
                    </View>
                  </View>

                  <RouteSpine from={trip.data.origin_label} to={trip.data.destination_label} />

                  <PortNotice endsAtPort={trip.data.ends_at_port} />

                  <View style={styles.stats}>
                    <SeatPips
                      free={trip.data.seats_free}
                      total={trip.data.seats_free + trip.data.passengers.length}
                      testID="seat-pips"
                    />
                    <Text style={styles.meta}>{trip.data.route_km.toFixed(1)} χλμ</Text>
                    {trip.data.price_eur > 0 ? (
                      <Text style={styles.price}>
                        {trip.data.price_eur.toFixed(2)} €{' '}
                        <Text style={styles.meta}>{t('ask.suggested')}</Text>
                      </Text>
                    ) : null}
                  </View>

                  {trip.data.price_breakdown ? (
                    <PriceBreakdown testID="trip-price" breakdown={trip.data.price_breakdown} />
                  ) : null}

                  {/* The route as it was snapped, not as the two labels suggest.
                      A trip cut short at a ferry port is the case where the
                      difference matters most. */}
                  {haveTiles() ? (
                    <Map
                      testID="trip-map"
                      centre={startOf(trip.data.route_polyline)}
                      polyline={trip.data.route_polyline}
                    />
                  ) : null}
                </Card>

                <Card>
                  <PersonLink
                    testID={`person-${trip.data.driver.id}`}
                    self={trip.data.driver.id === me?.id}
                    label={t('trip.openProfile', { name: trip.data.driver.name })}
                    onPress={() => openPerson(trip.data.driver)}
                  >
                  <View style={styles.person}>
                    <Avatar
                      name={trip.data.driver.name}
                      photoUrl={trip.data.driver.photo_url}
                      size={48}
                    />
                    <View style={styles.personText}>
                      <Text style={styles.personName}>
                        {trip.data.driver.name}
                        {trip.data.driver.rating ? ` · ★ ${trip.data.driver.rating}` : ''}
                      </Text>
                      <Text style={styles.meta}>
                        {trip.data.driver.car_model}
                        {trip.data.driver.car_color ? ` · ${trip.data.driver.car_color}` : ''}
                      </Text>
                      <SharedGroups testID="driver-shared-groups" names={trip.data.driver.shared_groups} />
                    </View>
                    {/* The plate only exists in this payload once a booking is made. */}
                    {trip.data.driver.car_plate ? (
                      <View testID="car-plate" style={styles.plate}>
                        <Text style={styles.plateText}>{trip.data.driver.car_plate}</Text>
                      </View>
                    ) : null}
                  </View>
                  </PersonLink>
                  {/* Outside the pressable card, so tapping a link opens the
                      link rather than the profile. */}
                  <SocialLinks
                    facebook={trip.data.driver.facebook_url}
                    instagram={trip.data.driver.instagram_url}
                  />

                  {/* What the two hours will be like, which the photo and the
                      rating do not say. */}
                  <PreferenceChips
                    testID="driver-preferences"
                    bio={trip.data.driver.bio}
                    tags={trip.data.driver.preferences}
                  />
                </Card>

                {/* Who is coming, for the driver. Their name and the groups
                    the two of them share, which is what decides whether a
                    stranger at the stop is expected. */}
                {trip.data.role === 'driver' && trip.data.passengers.length > 0 ? (
                  <Card>
                    {trip.data.passengers.map((p) => (
                      <PersonLink
                        key={p.id}
                        testID={`person-${p.id}`}
                        self={p.id === me?.id}
                        label={t('trip.openProfile', { name: p.name })}
                        onPress={() => openPerson(p)}
                      >
                      <View testID={`passenger-${p.id}`} style={styles.person}>
                        <Avatar name={p.name} photoUrl={p.photo_url} size={40} />
                        <View style={styles.personText}>
                          <Text style={styles.personName}>
                            {p.name}
                            {p.rating ? ` · ★ ${p.rating}` : ''}
                          </Text>
                          <SharedGroups names={p.shared_groups} />
                        </View>
                      </View>
                      </PersonLink>
                    ))}
                  </Card>
                ) : null}

                {/* Only for a trip that is still going to happen. */}
                {phase !== 'cancelled' ? <LiveCard tripId={id} /> : null}

                {/* The oldest safety feature there is: tell somebody who you
                    are with. A sentence for the share sheet, nothing stored. */}
                <Button
                  testID="trip-share"
                  label={t('tripShare.button')}
                  variant="secondary"
                  onPress={() =>
                    void shareText(tripShareMessage(trip.data, t))
                      .then((how) => how === 'copied' && setNotice(t('share.copied')))
                      .catch(() => setNotice(t('common.error')))
                  }
                />

                <RepeatCard trip={trip.data} />

                {notice ? (
                  <Text testID="trip-notice" style={styles.notice}>
                    {notice}
                  </Text>
                ) : null}

                {/* Which questions belong here depends on when it is and who
                    is asking. Before the trip has set off, whether it happened
                    is not a question anybody can answer; what the people on it
                    need is a way out. After it, the question is whether it
                    happened, and then how it was. A cancelled trip asks
                    nothing at all. */}
                {phase === 'after' && !trip.data.confirmed_by_me ? (
                  <Button
                    testID="trip-confirm"
                    label={t('trip.confirm')}
                    onPress={() => confirm.mutate()}
                    disabled={confirm.isPending}
                  />
                ) : null}

                {phase === 'after' && toRate.length > 0 ? (
                  <Card>
                    <Text style={styles.cardLabel}>{t('trip.rate')}</Text>
                    {toRate.map((p) => (
                      <RatingRow
                        key={p.id}
                        tripId={id}
                        person={p}
                        rated={!!p.rated}
                        onRated={() => void queryClient.invalidateQueries({ queryKey: ['trips', id] })}
                      />
                    ))}
                  </Card>
                ) : null}

                {phase === 'before' && trip.data.role === 'driver' ? (
                  <Button
                    testID="trip-cancel"
                    label={t('trip.cancel')}
                    variant="danger"
                    disabled={cancel.isPending}
                    onPress={() =>
                      askFirst({
                        title: t('trip.cancel'),
                        message:
                          trip.data.passengers.length > 0
                            ? t('trip.cancelWithPassengers', { count: trip.data.passengers.length })
                            : t('trip.cancelConfirm'),
                        confirmLabel: t('trip.cancel'),
                        cancelLabel: t('common.cancel'),
                        destructive: true,
                        onConfirm: () => cancel.mutate(),
                      })
                    }
                  />
                ) : null}

                {phase === 'before' && trip.data.role === 'passenger' && trip.data.booking_id ? (
                  <Button
                    testID="trip-leave"
                    label={t('trip.leave')}
                    variant="danger"
                    disabled={leave.isPending}
                    onPress={() => {
                      const bookingId = trip.data.booking_id;
                      if (!bookingId) return;
                      askFirst({
                        title: t('trip.leave'),
                        message: t('trip.leaveConfirm', { name: trip.data.driver.name }),
                        confirmLabel: t('trip.leave'),
                        cancelLabel: t('common.cancel'),
                        destructive: true,
                        onConfirm: () => leave.mutate(bookingId),
                      });
                    }}
                  />
                ) : null}

              </View>
  ) : null;

  // On a laptop the conversation is a column of its own beside the trip,
  // rather than something scrolled to underneath it: the details stay in view
  // while the two people agree where to meet.
  if (wide) {
    return (
      <Screen column="wide">
        <AsyncState loading={trip.isLoading} error={trip.error} onRetry={() => void trip.refetch()}>
          {trip.data ? (
            <View style={styles.columns}>
              <ScrollView
                style={styles.detailsColumn}
                contentContainerStyle={styles.list}
                showsVerticalScrollIndicator={false}
              >
                {details}
              </ScrollView>
              <View style={styles.chatColumn}>
                <Text style={styles.chatLabel}>{t('trip.chat')}</Text>
                <FlatList
                  testID="message-list"
                  data={messages.data?.messages ?? []}
                  keyExtractor={(m) => m.id}
                  contentContainerStyle={styles.chatList}
                  showsVerticalScrollIndicator={false}
                  renderItem={renderMessage}
                />
                {composerView}
              </View>
            </View>
          ) : null}
        </AsyncState>
      </Screen>
    );
  }

  return (
    <Screen>
      <AsyncState loading={trip.isLoading} error={trip.error} onRetry={() => void trip.refetch()}>
        {trip.data ? (
          <FlatList
            testID="message-list"
            data={messages.data?.messages ?? []}
            keyExtractor={(m) => m.id}
            contentContainerStyle={styles.list}
            showsVerticalScrollIndicator={false}
            ListHeaderComponent={
              <>
                {details}
                <Text style={styles.chatLabel}>{t('trip.chat')}</Text>
              </>
            }
            renderItem={renderMessage}
          />
        ) : null}
      </AsyncState>

      {sendFailedLine}
      <View style={styles.composer}>
        <TextInput
          testID="message-input"
          style={styles.composerInput}
          value={draft}
          onChangeText={setDraft}
          placeholder={t('trip.messagePlaceholder')}
          placeholderTextColor={colors.muted}
        />
        <Button
          testID="message-send"
          label={t('trip.send')}
          onPress={() => send.mutate(draft)}
          disabled={!draft.trim() || send.isPending}
        />
      </View>
    </Screen>
  );
}


const styles = StyleSheet.create({
  list: { gap: space.sm, paddingBottom: space.lg },
  columns: { flex: 1, flexDirection: 'row', gap: space.xl },
  detailsColumn: { flex: 1 },
  chatColumn: {
    flex: 1,
    gap: space.sm,
    backgroundColor: colors.sunken,
    borderRadius: radius.card,
    padding: space.lg,
    marginBottom: space.lg,
  },
  chatList: { gap: space.sm, flexGrow: 1, justifyContent: 'flex-end' },
  head: { gap: space.md, paddingBottom: space.sm },

  hero: { flexDirection: 'row', gap: space.lg, alignItems: 'center' },
  heroSide: { flex: 1, gap: space.xs, alignItems: 'flex-start' },
  time: { ...type.display, fontSize: 34, color: colors.ink },
  date: { ...type.caption, color: colors.muted },

  stats: { flexDirection: 'row', alignItems: 'center', gap: space.md },
  meta: { ...type.caption, color: colors.muted },
  price: { ...type.label, color: colors.primary, marginLeft: 'auto' },

  person: { flexDirection: 'row', alignItems: 'center', gap: space.md },
  personText: { flex: 1, gap: 2 },
  personName: { ...type.body, color: colors.ink },
  plate: {
    borderRadius: radius.inner,
    backgroundColor: colors.sunken,
    paddingHorizontal: space.md,
    paddingVertical: space.sm,
  },
  plateText: { ...type.mono, color: colors.ink },

  notice: { ...type.body, color: colors.danger },
  sendFailed: { ...type.caption, color: colors.danger, marginBottom: space.xs },
  badges: { flexDirection: 'row', flexWrap: 'wrap', gap: space.xs },
  cardLabel: { ...type.label, color: colors.muted },
  chatLabel: { ...type.label, color: colors.muted, marginTop: space.sm },

  bubbleRow: { alignItems: 'flex-start' },
  bubbleRowMine: { alignItems: 'flex-end' },
  bubble: {
    maxWidth: '82%',
    backgroundColor: colors.surface,
    borderRadius: radius.inner,
    borderBottomLeftRadius: 4,
    paddingHorizontal: space.md,
    paddingVertical: space.sm,
    gap: 2,
  },
  bubbleMine: {
    backgroundColor: colors.primary,
    borderBottomLeftRadius: radius.inner,
    borderBottomRightRadius: 4,
  },
  sender: { ...type.caption, fontSize: 12, color: colors.muted },
  bubbleText: { ...type.body, color: colors.ink },
  bubbleTextMine: { color: colors.primaryInk },
  bubbleTime: { ...type.caption, fontSize: 11, color: colors.muted, alignSelf: 'flex-end' },
  bubbleTimeMine: { color: 'rgba(246,245,241,0.7)' },

  composer: {
    flexDirection: 'row',
    gap: space.sm,
    alignItems: 'center',
    paddingBottom: space.lg,
  },
  composerInput: {
    flex: 1,
    ...type.body,
    color: colors.ink,
    backgroundColor: colors.surface,
    borderRadius: radius.pill,
    paddingHorizontal: space.lg,
    paddingVertical: space.md,
  },
});

// A finished trip where the caller has confirmed but somebody they could rate
// has not: the one state in which this screen changes because of someone else.
function awaitingOthers(trip: TripDetail | undefined): boolean {
  if (!trip || trip.status === 'cancelled' || !trip.confirmed_by_me) return false;
  if (Date.now() < new Date(trip.depart_latest).getTime()) return false;
  const others = trip.role === 'driver' ? trip.passengers : [trip.driver];
  return others.some((p) => !p.can_rate && !p.rated);
}

// Where the trip stands, said once, in the hero. A cancelled trip used to look
// exactly like a live one.
function TripStatus({
  phase,
  confirmed,
}: {
  phase: 'cancelled' | 'before' | 'underway' | 'after';
  confirmed: boolean;
}) {
  const { t } = useTranslation();
  const [label, tone] =
    phase === 'cancelled'
      ? [t('trip.statusCancelled'), 'danger' as const]
      : phase === 'before'
        ? [t('trip.statusScheduled'), 'quiet' as const]
        : phase === 'underway'
          ? [t('trip.statusUnderway'), 'accent' as const]
          : confirmed
            ? [t('trip.statusConfirmed'), 'success' as const]
            : [t('trip.statusToConfirm'), 'accent' as const];
  return <Badge testID="trip-status" label={label} tone={tone} />;
}

// Somebody on the trip, as a card row that opens their profile, where they can
// be blocked or reported. Your own row is not a link: there is nothing there
// for you to do to yourself.
function PersonLink({
  self,
  label,
  onPress,
  testID,
  children,
}: {
  self: boolean;
  label: string;
  onPress: () => void;
  testID: string;
  children: ReactNode;
}) {
  if (self) return <>{children}</>;
  return (
    <Pressable
      testID={testID}
      accessibilityRole="button"
      accessibilityLabel={label}
      onPress={onPress}
      style={({ pressed }) => (pressed ? { opacity: 0.7 } : undefined)}
    >
      {children}
    </Pressable>
  );
}

// Where to point the map: the first coordinate of the route as it was snapped.
// The two endpoint labels are names, not positions, and the polyline is the
// only thing on the trip that knows where it actually starts.
function startOf(encoded: string) {
  const [first] = polyline.decode(encoded, 6);
  return first ? { lon: first[1], lat: first[0] } : { lon: 23.7275, lat: 37.9838 };
}

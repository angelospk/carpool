import { Fragment, useEffect, useRef, useState, type ReactNode } from 'react';
import {
  FlatList,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
} from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import type { TFunction } from 'i18next';

import { api, type Message, type TripDetail, type TripStop } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { useSession } from '../../src/state/session';
import {
  AsyncState,
  Avatar,
  Badge,
  Button,
  Card,
  Screen,
  SeatPips,
  useBreakpoint,
} from '../../src/components/ui';
import { RouteGlyph, decodeRoute } from '../../src/components/RouteGlyph';
import { RepeatCard } from '../../src/components/RepeatCard';
import { PendingAsks } from '../../src/components/PendingAsks';
import { RatingRow } from '../../src/components/RatingRow';
import { SocialLinks } from '../../src/components/SocialLink';
import { CallButton } from '../../src/components/CallButton';
import { PortNotice } from '../../src/components/PortNotice';
import { PriceBreakdown } from '../../src/components/PriceBreakdown';
import { PreferenceChips } from '../../src/components/Preferences';
import { SharedGroups } from '../../src/components/SharedGroups';
import { LiveCard } from '../../src/components/LiveCard';
import { DriveCard, driveCardShown } from '../../src/components/DriveCard';
import { OnTheRoadCard, onTheRoad } from '../../src/components/OnTheRoadCard';
import { tripShareMessage } from '../../src/design/tripShare';
import { confirm as askFirst } from '../../src/components/confirm';
import { shareText } from '../../src/components/shareText';
import { Map } from '../../src/map/Map';
import polyline from '@mapbox/polyline';
import { haveTiles } from '../../src/map/tiles';
import { border, colors, radius, size, space, type } from '../../src/design/tokens';
import { athensDay, athensTime, eur, km, seatPriceNote } from '../../src/design/format';
import { approxArrival } from '../../src/design/ticket';
import { Ticket } from '../../src/components/Ticket';
import { liveWindow, liveWindowOpen } from '../../src/design/liveWindow';
import { tripPollInterval } from '../../src/design/tripPolling';

// How far from the end still counts as «at the bottom», in px: a bubble's
// worth of slack so a half-scrolled last message does not read as scrolled up.
const NEAR_BOTTOM = 80;

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
    // Other people change this screen: somebody leaves or is let on before the
    // trip, and after it rating opens once they confirm too. See
    // tripPollInterval for when it is worth asking again.
    refetchInterval: (q) => tripPollInterval(q.state.data, Date.now()),
  });
  const messages = useQuery({
    queryKey: ['trips', id, 'messages'],
    queryFn: () => api.messages(id),
    // Chat is polled while the screen is open. The websocket is the fast path;
    // this is the one that always works.
    refetchInterval: 10_000,
  });

  // Keeping the newest message in view. The composer sits outside the list, so
  // nothing covers the last bubble; what was missing is moving to it. A reader
  // at the bottom follows new messages down; one who scrolled up to reread
  // something is not yanked away, and is told instead.
  const listRef = useRef<FlatList<Message>>(null);
  const metrics = useRef({ y: 0, view: 0, content: 0 });
  const [hasNew, setHasNew] = useState(false);
  const nearBottom = () => {
    const m = metrics.current;
    return m.content - (m.y + m.view) < NEAR_BOTTOM;
  };
  const scrollToEnd = () => requestAnimationFrame(() => listRef.current?.scrollToEnd({ animated: true }));
  // The opening fetch is not a «new» message, so nothing above scrolls to it:
  // a long history would open on its oldest messages. The list stays pinned to
  // its end until the reader scrolls away: bubbles of different heights are
  // measured after the first layout and the content keeps growing, so one
  // scroll after the first layout left the newest message off-screen.
  const pinnedToEnd = useRef(true);
  const onListScroll = (e: NativeSyntheticEvent<NativeScrollEvent>) => {
    const { contentOffset, layoutMeasurement, contentSize } = e.nativeEvent;
    metrics.current = { y: contentOffset.y, view: layoutMeasurement.height, content: contentSize.height };
    pinnedToEnd.current = nearBottom();
    if (pinnedToEnd.current) setHasNew(false);
  };
  // undefined until the first load: the opening fetch is not a «new» message.
  const lastSeen = useRef<string | null | undefined>(undefined);
  const lastMessage = messages.data?.messages.at(-1);
  const lastId = messages.data ? (lastMessage?.id ?? null) : undefined;
  useEffect(() => {
    if (lastId === undefined) return;
    const before = lastSeen.current;
    lastSeen.current = lastId;
    if (before === undefined || lastId === null || lastId === before) return;
    // Own messages always follow (that covers sending), others only when the
    // reader is already at the end.
    if (lastMessage?.sender_id === me?.id || nearBottom()) {
      setHasNew(false);
      scrollToEnd();
    } else {
      setHasNew(true);
    }
  }, [lastId]); // eslint-disable-line react-hooks/exhaustive-deps
  const listProps = {
    ref: listRef,
    testID: 'message-list',
    onScroll: onListScroll,
    scrollEventThrottle: 100,
    onLayout: (e: { nativeEvent: { layout: { height: number } } }) => {
      metrics.current.view = e.nativeEvent.layout.height;
    },
    onContentSizeChange: (_w: number, h: number) => {
      metrics.current.content = h;
      if (pinnedToEnd.current && messages.data?.messages.length) {
        requestAnimationFrame(() => listRef.current?.scrollToEnd({ animated: false }));
      }
    },
  };
  const newMessagePill = hasNew ? (
    <Pressable
      testID="new-message"
      accessibilityRole="button"
      style={styles.newMessage}
      onPress={() => {
        setHasNew(false);
        scrollToEnd();
      }}
    >
      <Text style={styles.newMessageText}>{t('trip.newMessage')}</Text>
    </Pressable>
  ) : null;

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
  // Raised by the drive card, shown here: pressing «Φεύγω» swaps that card for
  // the on-the-road one, and a warning kept inside it would go with it.
  const [trackingWarning, setTrackingWarning] = useState<string | null>(null);
  const earliestAt = trip.data ? new Date(trip.data.depart_earliest).getTime() : null;
  const latestAt = trip.data ? new Date(trip.data.depart_latest).getTime() : null;
  // The sharing window's two ends are edges too: «Είμαι εδώ» appears at one
  // and goes at the other.
  const sharing = trip.data ? liveWindow(trip.data) : null;
  const nextEdge =
    [sharing?.opens ?? null, earliestAt, latestAt, sharing?.closes ?? null]
      .filter((at): at is number => at !== null && at > now)
      .sort((a, b) => a - b)[0] ?? null;
  useEffect(() => {
    if (nextEdge === null) return;
    const timer = setTimeout(() => setNow(Date.now()), nextEdge - now + 1000);
    return () => clearTimeout(timer);
  }, [nextEdge, now]);
  // A drive pins the window to one minute, so by the clock alone the trip
  // would be over the moment the car leaves, asking «Έγινε;» of a driver who
  // has just set off. On the road it is under way, until «Τέλος».
  const phase: 'cancelled' | 'before' | 'underway' | 'after' =
    trip.data?.status === 'cancelled'
      ? 'cancelled'
      : trip.data && onTheRoad(trip.data, now)
        ? 'underway'
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

  // «Είμαι εδώ»: the passenger is at the pickup, and the driver is told.
  const here = useMutation({
    mutationFn: () => api.here(id),
    onSuccess: () => void trip.refetch(),
  });

  const cancel = useMutation({
    mutationFn: () => api.cancelTrip(id),
    onSuccess: () => {
      setNotice(t('trip.cancelled'));
      void trip.refetch();
      // The routes screen counts upcoming departures per route, and one of them
      // just stopped being upcoming.
      void queryClient.invalidateQueries({ queryKey: ['my-routes'] });
      void queryClient.invalidateQueries({ queryKey: ['trips'] });
      void queryClient.invalidateQueries({ queryKey: ['asks'] });
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
      void queryClient.invalidateQueries({ queryKey: ['supply'] });
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
                {/* Laid out as the ticket on the home screen that opened it,
                    so the trip does not change shape between the list and
                    its own page. */}
                <Ticket
                  header={
                    <>
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
                    </>
                  }
                  depart={{ time: athensTime(trip.data.depart_earliest), place: trip.data.origin_label }}
                  // No driving time comes with a trip, so the arrival is worked
                  // out from the distance and marked «≈».
                  arrive={{
                    time: athensTime(approxArrival(trip.data.depart_earliest, trip.data.route_km)),
                    place: trip.data.destination_label,
                    approx: true,
                  }}
                  distance={km(trip.data.route_km)}
                  aside={
                    <RouteGlyph
                      points={decodeRoute(trip.data.route_polyline)}
                      seed={`${trip.data.origin_label}${trip.data.destination_label}`}
                      width={72}
                      height={44}
                      testID="route-glyph"
                    />
                  }
                  // The lower half only when it has something to say: the seats
                  // left in the driver's car, or the contribution.
                  lowerLeft={
                    Number.isFinite(trip.data.seats_free) ? (
                      // Dots alone do not say whether they count free or taken seats.
                      <View style={styles.seatsRow}>
                        <SeatPips
                          free={trip.data.seats_free}
                          total={
                            trip.data.seats_total ??
                            trip.data.seats_free + trip.data.passengers.reduce((n, p) => n + (p.seats ?? 1), 0)
                          }
                          testID="seat-pips"
                        />
                        <Text testID="seats-free" style={styles.meta}>
                          {t('home.seatsFree', { count: trip.data.seats_free })}
                        </Text>
                      </View>
                    ) : undefined
                  }
                  fare={trip.data.price_eur > 0 ? eur(trip.data.price_eur) : undefined}
                  fareLabel={
                    trip.data.price_eur > 0
                      ? seatPriceNote(t, trip.data.price_eur, trip.data.seats) ?? t('ask.suggested')
                      : undefined
                  }
                />

                  {trip.data.pickup_at ? (
                    <Text testID="pickup-at" style={styles.pickupAt}>
                      {t('drive.pickupAt', { time: athensTime(trip.data.pickup_at) })}
                    </Text>
                  ) : null}

                  <PortNotice endsAtPort={trip.data.ends_at_port} />

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
                  <CallButton phone={trip.data.driver.phone} until={trip.data.driver.call_until} />

                  {/* What the two hours will be like, which the photo and the
                      rating do not say. */}
                  <PreferenceChips
                    testID="driver-preferences"
                    bio={trip.data.driver.bio}
                    tags={trip.data.driver.preferences}
                  />
                </Card>

                {/* Who is in the car, for everybody in it: the driver sees who
                    is coming, and a passenger who else is. Name, and the groups
                    the caller shares with each, which is what decides whether
                    a stranger at the stop is expected. Where each one is
                    picked up stays between them and the driver. */}
                {trip.data.passengers.length > 0 ? (
                  <Card>
                    <Text style={styles.label}>{t('trip.inTheCar')}</Text>
                    {trip.data.passengers.map((p) => (
                      <Fragment key={p.id}>
                      <PersonLink
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
                            {p.id === me?.id ? ` · ${t('trip.you')}` : ''}
                            {p.rating ? ` · ★ ${p.rating}` : ''}
                          </Text>
                          <SharedGroups names={p.shared_groups} />
                        </View>
                        {trip.data.role === 'driver' && p.arrived_at ? (
                          <Badge testID={`at-pickup-${p.id}`} label={t('drive.atPickup')} tone="accent" />
                        ) : null}
                      </View>
                      </PersonLink>
                      {/* The driver reaches each passenger as a passenger reaches
                          the driver; passengers do not reach each other. */}
                      {trip.data.role === 'driver' && p.id !== me?.id ? (
                        <>
                          <SocialLinks
                            testID={`social-links-${p.id}`}
                            facebook={p.facebook_url}
                            instagram={p.instagram_url}
                          />
                          <CallButton phone={p.phone} until={p.call_until} testID={`call-${p.id}`} />
                        </>
                      ) : null}
                      </Fragment>
                    ))}
                  </Card>
                ) : null}

                {/* In road order. The ticket already has the car's two ends,
                    so only the stops between are listed. */}
                {trip.data.stops && trip.data.stops.length > 0 ? (
                  <Card>
                    <Text style={styles.label}>{t('trip.stops')}</Text>
                    {trip.data.stops.map((s, i) => (
                      <View key={i} testID={`stop-${i}`} style={styles.stop}>
                        <Text style={styles.stopHead}>
                          {s.at ? `${athensTime(s.at)} · ` : ''}
                          {s.kind === 'pickup' ? t('trip.stopPickup') : t('trip.stopDropoff')}
                        </Text>
                        <Text style={styles.meta}>{stopDetail(s, trip.data.role, t)}</Text>
                      </View>
                    ))}
                  </Card>
                ) : null}

                {trackingWarning && (onTheRoad(trip.data, now) || driveCardShown(trip.data, now)) ? (
                  <Text testID="tracking-warning" style={styles.trackingWarning}>
                    {trackingWarning}
                  </Text>
                ) : null}
                {onTheRoad(trip.data, now) ? (
                  <OnTheRoadCard trip={trip.data} onChanged={() => void trip.refetch()} />
                ) : driveCardShown(trip.data, now) ? (
                  <DriveCard
                    trip={trip.data}
                    onDone={() => void trip.refetch()}
                    onTrackingWarning={setTrackingWarning}
                  />
                ) : null}

                {herePossible(trip.data, me?.id, now) ? (
                  <View style={styles.here}>
                    {ownSeat(trip.data, me?.id)?.arrived_at || here.isSuccess ? (
                      <Text testID="here-done" style={styles.pickupAt}>
                        {t('drive.hereDone')}
                      </Text>
                    ) : (
                      <Button
                        testID="here"
                        label={t('drive.here')}
                        disabled={here.isPending}
                        onPress={() => here.mutate()}
                      />
                    )}
                  </View>
                ) : null}

                {/* Only for a trip that is still going to happen. */}
                {phase !== 'cancelled' ? (
                  <LiveCard
                    tripId={id}
                    role={trip.data.role}
                    routePolyline={trip.data.route_polyline}
                    pickup={trip.data.stops?.find((s) => s.mine && s.kind === 'pickup')?.point ?? null}
                    // Only a phone runs the drive's background task; a
                    // browser shares nothing on its own, so it keeps the
                    // ordinary button rather than claiming otherwise.
                    carShared={
                      Platform.OS !== 'web' &&
                      trip.data.role === 'driver' &&
                      !!trip.data.drive_set_at &&
                      !trip.data.drive_ended_at
                    }
                  />
                ) : null}

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

                {/* A driver is told here too: the question lapses, and the home
                    screen is not where they are looking. */}
                {trip.data.role === 'driver' ? <PendingAsks occurrenceId={trip.data.id} /> : null}

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
                    variant="quietDanger"
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
                    variant="quietDanger"
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
                  {...listProps}
                  data={messages.data?.messages ?? []}
                  keyExtractor={(m) => m.id}
                  contentContainerStyle={styles.chatList}
                  showsVerticalScrollIndicator={false}
                  renderItem={renderMessage}
                />
                {newMessagePill}
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
            {...listProps}
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

      {newMessagePill}
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
  label: { ...type.caption, color: colors.muted, marginBottom: space.xs },
  trackingWarning: { ...type.body, color: colors.danger },
  seatsRow: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
  stop: { gap: 2, marginBottom: space.sm },
  stopHead: { ...type.label, color: colors.ink },
  pickupAt: { ...type.label, fontSize: 16, color: colors.ink, marginTop: space.sm },
  here: { marginTop: space.sm },
  list: { gap: space.sm, paddingBottom: space.lg },
  columns: { flex: 1, flexDirection: 'row', gap: space.xl },
  detailsColumn: { flex: 1 },
  chatColumn: {
    flex: 1,
    gap: space.sm,
    backgroundColor: colors.surface,
    borderRadius: radius.card,
    padding: space.lg,
    marginBottom: space.lg,
  },
  chatList: { gap: space.sm, flexGrow: 1, justifyContent: 'flex-end' },
  head: { gap: space.md, paddingBottom: space.sm },

  date: { ...type.label, color: colors.ink, flexShrink: 1 },
  meta: { ...type.caption, color: colors.muted },

  person: { flexDirection: 'row', alignItems: 'center', gap: space.md },
  personText: { flex: 1, gap: 2 },
  personName: { ...type.heading, color: colors.ink },
  plate: {
    borderRadius: radius.chip,
    borderWidth: border.selected,
    borderColor: colors.ink,
    paddingHorizontal: space.md,
    paddingVertical: space.sm,
  },
  plateText: { ...type.label, color: colors.ink },

  notice: { ...type.body, color: colors.danger },
  sendFailed: { ...type.caption, color: colors.danger, marginBottom: space.xs },
  badges: { flexDirection: 'row', flexWrap: 'wrap', justifyContent: 'flex-end', gap: space.xs, flexShrink: 1 },
  cardLabel: { ...type.label, color: colors.muted },
  chatLabel: { ...type.label, color: colors.muted, marginTop: space.sm },

  bubbleRow: { alignItems: 'flex-start' },
  bubbleRowMine: { alignItems: 'flex-end' },
  bubble: {
    maxWidth: '82%',
    backgroundColor: colors.paper,
    borderWidth: border.rest,
    borderColor: colors.line,
    borderRadius: radius.control,
    borderBottomLeftRadius: 4,
    paddingHorizontal: space.md,
    paddingVertical: space.sm,
    gap: 2,
  },
  bubbleMine: {
    backgroundColor: colors.ink,
    borderBottomLeftRadius: radius.control,
    borderBottomRightRadius: 4,
  },
  sender: { ...type.caption, fontSize: 12, color: colors.muted },
  bubbleText: { ...type.body, color: colors.ink },
  bubbleTextMine: { color: colors.paper },
  bubbleTime: { ...type.caption, fontSize: 11, color: colors.muted, alignSelf: 'flex-end' },
  bubbleTimeMine: { color: 'rgba(255,255,255,0.7)' },

  newMessage: {
    alignSelf: 'center',
    backgroundColor: colors.ink,
    borderRadius: radius.chip,
    paddingHorizontal: space.lg,
    paddingVertical: space.sm,
    marginBottom: space.sm,
  },
  newMessageText: { ...type.label, color: colors.paper },

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
    backgroundColor: colors.paper,
    borderWidth: border.rest,
    borderColor: colors.line,
    borderRadius: radius.control,
    minHeight: size.cta,
    paddingHorizontal: space.lg,
    paddingVertical: space.md,
  },
});

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

// What a stop says under its time. The driver sees everyone; a passenger sees
// their own meeting point and, for everyone else, only that somebody gets on or
// off there — the server sends nothing more, and neither do we invent it.
function stopDetail(s: TripStop, role: TripDetail['role'], t: TFunction): string {
  if (role !== 'driver') {
    return s.mine ? [t('trip.stopYou'), s.label].filter(Boolean).join(' · ') : t('trip.stopOther');
  }
  return [
    s.name,
    s.label,
    s.seats ? t('trip.stopSeats', { count: s.seats }) : null,
    s.luggage_count && s.luggage_count > 0 ? t('trip.stopLuggage', { count: s.luggage_count }) : null,
  ]
    .filter(Boolean)
    .join(' · ');
}

/** The caller's own place on the trip, as a passenger. */
function ownSeat(trip: TripDetail, me: string | undefined) {
  return trip.passengers.find((p) => p.id === me);
}

/**
 * «Είμαι εδώ» belongs to a passenger with a seat on a trip that is still going:
 * a drive the driver has not ended with «Τέλος», or a scheduled trip inside the
 * window live sharing is open for.
 */
function herePossible(trip: TripDetail, me: string | undefined, now: number): boolean {
  if (trip.role !== 'passenger' || trip.status !== 'scheduled' || !ownSeat(trip, me)) return false;
  if (trip.drive_set_at) return !trip.drive_ended_at;
  // An older server sends no route_min, and a window guessed with zero
  // minutes of road would close before the car arrives.
  if (trip.route_min == null) return false;
  return liveWindowOpen(trip, now);
}

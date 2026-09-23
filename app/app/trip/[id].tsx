import { useEffect, useState } from 'react';
import { Alert, FlatList, StyleSheet, Text, TextInput, View } from 'react-native';
import { useLocalSearchParams } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '../../src/api/endpoints';
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
} from '../../src/components/ui';
import { RouteGlyph, decodeRoute } from '../../src/components/RouteGlyph';
import { RepeatCard } from '../../src/components/RepeatCard';
import { SocialLinks } from '../../src/components/SocialLink';
import { PortNotice } from '../../src/components/PortNotice';
import { PriceBreakdown } from '../../src/components/PriceBreakdown';
import { PreferenceChips } from '../../src/components/Preferences';
import { SharedGroups } from '../../src/components/SharedGroups';
import { Map } from '../../src/map/Map';
import polyline from '@mapbox/polyline';
import { haveTiles } from '../../src/map/tiles';
import { colors, radius, space, type } from '../../src/design/tokens';
import { athensDay, athensTime } from '../../src/design/format';

export default function TripScreen() {
  const { t } = useTranslation();
  const { id } = useLocalSearchParams<{ id: string }>();
  const queryClient = useQueryClient();
  const me = useSession((s) => s.me);
  const [draft, setDraft] = useState('');
  const [notice, setNotice] = useState<string | null>(null);

  const trip = useQuery({ queryKey: ['trips', id], queryFn: () => api.trip(id) });
  const messages = useQuery({
    queryKey: ['trips', id, 'messages'],
    queryFn: () => api.messages(id),
    // Chat is polled while the screen is open. The websocket is the fast path;
    // this is the one that always works.
    refetchInterval: 10_000,
  });

  const send = useMutation({
    mutationFn: (body: string) => api.postMessage(id, body),
    onSuccess: () => {
      setDraft('');
      void queryClient.invalidateQueries({ queryKey: ['trips', id, 'messages'] });
    },
  });

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

  // The trip has set off when its earliest departure is behind us. Compared
  // against the device clock, which is the only one the screen has; the server
  // decides for real and answers not_departed if this is optimistic.
  //
  // Re-evaluated on a timer as well as on render, because somebody sitting on
  // this screen at 07:29 waiting to set off would otherwise still be offered
  // cancel at 07:31 and no way to confirm.
  const [now, setNow] = useState(() => Date.now());
  const departAt = trip.data ? new Date(trip.data.depart_earliest).getTime() : null;
  useEffect(() => {
    if (departAt === null || departAt <= now) return;
    const timer = setTimeout(() => setNow(Date.now()), departAt - now + 1000);
    return () => clearTimeout(timer);
  }, [departAt, now]);
  const departed = departAt !== null && departAt <= now;
  const hasCompany =
    trip.data?.role === 'driver' ? (trip.data.passengers.length ?? 0) > 0 : true;

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

  const rate = useMutation({
    mutationFn: (stars: number) =>
      api.rateTrip(id, stars, trip.data?.role === 'driver' ? trip.data.passengers[0]?.id : undefined),
    onSuccess: () => setNotice(t('trip.rated')),
    onError: () => setNotice(t('common.error')),
  });

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
                      <Badge
                        label={
                          trip.data.role === 'driver' ? t('home.asDriver') : t('home.asPassenger')
                        }
                        tone={trip.data.role === 'driver' ? 'primary' : 'quiet'}
                      />
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
                      <SocialLinks
                        facebook={trip.data.driver.facebook_url}
                        instagram={trip.data.driver.instagram_url}
                      />
                      <SharedGroups testID="driver-shared-groups" names={trip.data.driver.shared_groups} />
                    </View>
                    {/* The plate only exists in this payload once a booking is made. */}
                    {trip.data.driver.car_plate ? (
                      <View testID="car-plate" style={styles.plate}>
                        <Text style={styles.plateText}>{trip.data.driver.car_plate}</Text>
                      </View>
                    ) : null}
                  </View>

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
                      <View key={p.id} testID={`passenger-${p.id}`} style={styles.person}>
                        <Avatar name={p.name} photoUrl={p.photo_url} size={40} />
                        <View style={styles.personText}>
                          <Text style={styles.personName}>
                            {p.name}
                            {p.rating ? ` · ★ ${p.rating}` : ''}
                          </Text>
                          <SharedGroups names={p.shared_groups} />
                        </View>
                      </View>
                    ))}
                  </Card>
                ) : null}

                <RepeatCard trip={trip.data} />

                {notice ? (
                  <Text testID="trip-notice" style={styles.notice}>
                    {notice}
                  </Text>
                ) : null}

                {/* Which questions belong here depends on when it is and who
                    is asking. Before the trip has set off, whether it happened
                    is not a question anybody can answer, and a driver with an
                    empty car has nothing to rate; what they need is a way to
                    call it off. Asking all four things at all times is how a
                    driver ended up being asked to rate a trip nobody had taken
                    yet. */}
                {departed ? (
                  <View style={styles.actions}>
                    <View style={styles.action}>
                      <Button
                        testID="trip-confirm"
                        label={t('trip.confirm')}
                        onPress={() => confirm.mutate()}
                        disabled={confirm.isPending}
                      />
                    </View>
                    {/* Nobody to rate in an empty car. */}
                    {hasCompany ? (
                      <View style={styles.action}>
                        <Button
                          testID="trip-rate"
                          label={`${t('trip.rate')} ★`}
                          variant="secondary"
                          onPress={() => rate.mutate(5)}
                          disabled={rate.isPending}
                        />
                      </View>
                    ) : null}
                  </View>
                ) : trip.data.role === 'driver' ? (
                  <Button
                    testID="trip-cancel"
                    label={t('trip.cancel')}
                    variant="danger"
                    disabled={cancel.isPending}
                    onPress={() =>
                      Alert.alert(
                        t('trip.cancel'),
                        trip.data.passengers.length > 0
                          ? t('trip.cancelWithPassengers', { count: trip.data.passengers.length })
                          : t('trip.cancelConfirm'),
                        [
                          { text: t('common.cancel'), style: 'cancel' },
                          {
                            text: t('trip.cancel'),
                            style: 'destructive',
                            onPress: () => cancel.mutate(),
                          },
                        ],
                      )
                    }
                  />
                ) : null}

                <Text style={styles.chatLabel}>{t('trip.chat')}</Text>
              </View>
            }
            renderItem={({ item }) => {
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
            }}
          />
        ) : null}
      </AsyncState>

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
  actions: { flexDirection: 'row', gap: space.sm },
  action: { flex: 1 },
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

// Where to point the map: the first coordinate of the route as it was snapped.
// The two endpoint labels are names, not positions, and the polyline is the
// only thing on the trip that knows where it actually starts.
function startOf(encoded: string) {
  const [first] = polyline.decode(encoded, 6);
  return first ? { lon: first[1], lat: first[0] } : { lon: 23.7275, lat: 37.9838 };
}

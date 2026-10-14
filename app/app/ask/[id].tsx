// The ask screen is the heart of the driver experience: it opens from a push
// notification and must answer "is this worth it?" in one glance and one
// round trip.

import { useEffect, useRef, useState } from 'react';
import { Linking, StyleSheet, Text, View } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { refusalText } from '../../src/api/errorText';
import {
  AsyncState,
  Badge,
  Button,
  Card,
  Screen,
  Spacer,
} from '../../src/components/ui';
import { colors, space, type } from '../../src/design/tokens';
import { athensTime, eur } from '../../src/design/format';
import { mapsUrl, remainingStops, type LonLat } from '../../src/drive/maps';
import { setDriveStops, visitedStops } from '../../src/drive/tracking';
import { decodeRoute } from '../../src/components/RouteGlyph';
import { getMapsApp } from '../../src/drive/mapsApp';
import { detourText } from '../../src/design/detour';
import { JourneySummary } from '../../src/components/JourneySummary';

export default function AskScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const queryClient = useQueryClient();
  // take=1 is «Πάρ' τον» on the notification: accept as soon as the offer
  // has loaded, and hand the pickup to the driver's maps app.
  const { id, take } = useLocalSearchParams<{ id: string; take?: string }>();
  const [outcome, setOutcome] = useState<string | null>(null);
  // The ask can no longer be answered: expired, taken, full, or gone. Other
  // failures may work on a second press, so they leave the buttons.
  const [closed, setClosed] = useState(false);

  // Loaded by id, because a notification carries nothing else. The screen used
  // to read the detour and price off its route params, so every way in except
  // one that nothing used showed a detour of zero minutes to nobody.
  const ask = useQuery({ queryKey: ['asks', id], queryFn: () => api.ask(id) });
  // Gone is a normal answer here: the push may be minutes old, and the ask
  // lapsed or somebody else took the seat.
  const gone = ask.error instanceof ApiError && ask.error.status === 404;

  const home = () => router.replace('/(tabs)');
  const answered = () => {
    void queryClient.invalidateQueries({ queryKey: ['trips'] });
    void queryClient.invalidateQueries({ queryKey: ['asks'] });
    home();
  };

  // A stale tap is normal: the push may be minutes old and the ask may have
  // expired or been taken. Each case gets its own message rather than a generic
  // failure.
  const onError = (e: unknown) => {
    void queryClient.invalidateQueries({ queryKey: ['asks', 'pending'] });
    if (!(e instanceof ApiError)) return setOutcome(t('common.error'));
    switch (e.code) {
      case 'ask_expired':
        setClosed(true);
        return setOutcome(t('ask.expired'));
      case 'no_seats':
        setClosed(true);
        return setOutcome(t('ask.noSeats'));
      case 'ask_closed':
        setClosed(true);
        return setOutcome(t('ask.closed'));
      case 'trip_departed':
      case 'not_yours':
        setClosed(true);
        return setOutcome(refusalText(e) ?? t('ask.closed'));
      default:
        return setOutcome(refusalText(e) ?? t('common.error'));
    }
  };

  const accept = useMutation({
    mutationFn: () => api.acceptAsk(id),
    onSuccess: (result) => {
      if (take === '1' && ask.data) return void handOff(ask.data.occurrence_id, result.booking_id);
      // The trip it made, rather than the home list with no word about it.
      if (!ask.data) return answered();
      void queryClient.invalidateQueries({ queryKey: ['trips'] });
      void queryClient.invalidateQueries({ queryKey: ['asks'] });
      router.replace({ pathname: '/trip/[id]', params: { id: ask.data.occurrence_id } });
    },
    onError,
  });

  // From the notification, in a car: the pickup goes straight to the maps app,
  // and the app is left on the trip, where the next stop and «Τέλος» are.
  const handOff = async (occurrenceId: string, bookingId: string) => {
    void queryClient.invalidateQueries({ queryKey: ['trips'] });
    void queryClient.invalidateQueries({ queryKey: ['asks'] });
    router.replace({ pathname: '/trip/[id]', params: { id: occurrenceId } });
    try {
      const trip = await api.trip(occurrenceId);
      const seat = trip.passengers.find((p) => p.booking_id === bookingId);
      if (!trip.destination || !seat?.pickup) return;
      // Every stop still ahead, not only the new one: a second passenger must
      // not take the first one's pickup off the route.
      await setDriveStops(
        trip.passengers.flatMap((p) => [p.pickup, p.dropoff]).filter((s): s is LonLat => !!s),
      );
      const stops = remainingStops(decodeRoute(trip.route_polyline), trip.passengers, await visitedStops());
      await Linking.openURL(mapsUrl(await getMapsApp(), { stops, destination: trip.destination }));
    } catch {
      // The seat is booked either way; the trip screen has the same button.
    }
  };
  const tried = useRef(false);
  useEffect(() => {
    if (take !== '1' || !ask.data || tried.current) return;
    tried.current = true;
    accept.mutate();
  }, [take, ask.data, accept]);
  const decline = useMutation({
    mutationFn: () => api.declineAsk(id),
    onSuccess: answered,
    onError,
  });

  const busy = accept.isPending || decline.isPending;
  // Once the answer is known to be refused, the two buttons are the wrong
  // question. A network failure is not that: it may work on a second press.
  const settled = gone || closed;

  return (
    <Screen>
      <AsyncState
        loading={ask.isLoading}
        error={gone ? null : ask.error}
        onRetry={() => void ask.refetch()}
      >
        {gone ? (
          <Card>
            <Text testID="ask-gone" style={styles.gone}>
              {t('ask.gone')}
            </Text>
          </Card>
        ) : ask.data ? (
          <>
            {/* The detour is the decision. It gets the size, and everything
                else arranges itself around it. */}
            <Card tone="accent">
              <Badge label={t('ask.waiting')} tone="accent" />
              <Text style={styles.who}>
                {t('ask.who', { name: ask.data.passenger_name, count: ask.data.seats })}
              </Text>
              <Text testID="ask-detour" style={styles.detour}>
                {detourText(ask.data.detour_min, ask.data.detour_km, t)}
              </Text>
              {ask.data.price_eur > 0 ? (
                <Text style={styles.price}>
                  {eur(ask.data.price_eur)}{' '}
                  <Text style={styles.suggested}>{t('ask.suggested')}</Text>
                </Text>
              ) : null}
            </Card>
            <Card>
              <JourneySummary
                from={ask.data.origin_label}
                to={ask.data.destination_label}
                start={ask.data.depart_earliest}
                status={t('ask.answerBy', { time: athensTime(ask.data.expires_at) })}
                statusTone="muted"
              />
            </Card>
          </>
        ) : null}
      </AsyncState>

      {outcome ? (
        <Text testID="ask-outcome" style={styles.outcome}>
          {outcome}
        </Text>
      ) : null}

      <Spacer />

      {settled ? (
        <Button testID="ask-leave" label={t('ask.backHome')} onPress={home} />
      ) : ask.data ? (
        <View style={styles.actions}>
          <Button
            testID="ask-accept"
            label={t('ask.accept')}
            onPress={() => accept.mutate()}
            disabled={busy}
          />
          <Button
            testID="ask-decline"
            label={t('ask.decline')}
            variant="secondary"
            onPress={() => decline.mutate()}
            disabled={busy}
          />
        </View>
      ) : null}
    </Screen>
  );
}

const styles = StyleSheet.create({
  detour: { ...type.display, fontSize: 32, color: colors.ink },
  price: { ...type.title, color: colors.ink },
  // Never "fare": the app suggests a contribution, it does not charge one.
  suggested: { ...type.caption, color: colors.muted },
  outcome: { ...type.body, color: colors.danger },
  who: { ...type.body, color: colors.ink },
  gone: { ...type.body, color: colors.ink },
  actions: { gap: space.sm },
});

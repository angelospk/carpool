// Live location during a trip, shared by choice and only while it matters.
//
// Foreground only, and said so on the screen: the position is sent while this
// screen is open and stops when it closes. Keeping it going in the background
// needs a background permission and a review of what that means on iOS, which
// SPEC.md defers. A promise the app cannot keep with the screen shut would be
// worse than a smaller one it can.
//
// The server decides who sees what and when (internal/live); this shows its
// answer, offers the button inside the window, and stops cleanly outside it.

import { useEffect, useRef, useState } from 'react';
import { Linking, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import * as Location from 'expo-location';

import { api } from '../api/endpoints';
import { ApiError } from '../api/client';
import { athensTime } from '../design/format';
import { colors, space, type } from '../design/tokens';
import { Button, Card, Hint } from './ui';

const POLL_MS = 15_000;

export function LiveCard({ tripId, now = () => Date.now() }: { tripId: string; now?: () => number }) {
  const { t } = useTranslation();
  const live = useQuery({
    queryKey: ['trips', tripId, 'live'],
    queryFn: () => api.live(tripId),
    refetchInterval: POLL_MS,
  });
  const [sharing, setSharing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const watch = useRef<Location.LocationSubscription | null>(null);

  const stop = async () => {
    watch.current?.remove();
    watch.current = null;
    setSharing(false);
    await api.stopPosition(tripId).catch(() => undefined);
  };

  // Leaving the screen stops sharing, which is what the hint promised.
  useEffect(
    () => () => {
      if (watch.current) {
        watch.current.remove();
        watch.current = null;
        void api.stopPosition(tripId).catch(() => undefined);
      }
    },
    [tripId],
  );

  const start = async () => {
    setError(null);
    const permission = await Location.requestForegroundPermissionsAsync();
    if (!permission.granted) {
      setError(t('live.denied'));
      return;
    }
    setSharing(true);
    watch.current = await Location.watchPositionAsync(
      { accuracy: Location.Accuracy.Balanced, timeInterval: POLL_MS, distanceInterval: 30 },
      (pos) => {
        api.putPosition(tripId, pos.coords.latitude, pos.coords.longitude).catch((e: unknown) => {
          // The window closed, or the trip was called off: stop rather than
          // keep sending into a refusal.
          if (e instanceof ApiError && (e.code === 'not_sharing_now' || e.code === 'not_found')) {
            void stop();
          }
        });
      },
    );
  };

  if (!live.data) return null;
  const opens = new Date(live.data.opens).getTime();
  const closes = new Date(live.data.closes).getTime();
  const at = now();

  return (
    <Card testID="live-card">
      <Text style={styles.title}>{t('live.title')}</Text>
      {at < opens ? (
        <Hint testID="live-not-yet">{t('live.notYet', { time: athensTime(live.data.opens) })}</Hint>
      ) : at >= closes ? (
        <Hint testID="live-over">{t('live.over')}</Hint>
      ) : (
        <>
          {live.data.positions.length === 0 ? (
            <Hint testID="live-nobody">{t('live.nobody')}</Hint>
          ) : (
            live.data.positions.map((p) => (
              <View key={p.user_id} testID={`live-${p.user_id}`} style={styles.row}>
                <Text style={styles.who}>
                  {t('live.ago', {
                    name: p.name,
                    minutes: Math.max(0, Math.round((at - new Date(p.updated_at).getTime()) / 60_000)),
                  })}
                </Text>
                <Button
                  testID={`live-map-${p.user_id}`}
                  label={t('live.openMap')}
                  variant="secondary"
                  onPress={() =>
                    void Linking.openURL(
                      `https://www.openstreetmap.org/?mlat=${p.lat}&mlon=${p.lon}#map=16/${p.lat}/${p.lon}`,
                    )
                  }
                />
              </View>
            ))
          )}
          {sharing ? (
            <>
              <Hint testID="live-sharing">{t('live.sharing')}</Hint>
              <Button testID="live-stop" label={t('live.stop')} variant="danger" onPress={() => void stop()} />
            </>
          ) : (
            <Button testID="live-start" label={t('live.start')} onPress={() => void start()} />
          )}
          <Hint>{t('live.who')}</Hint>
        </>
      )}
      {error ? (
        <Text testID="live-error" style={styles.error}>
          {error}
        </Text>
      ) : null}
    </Card>
  );
}

const styles = StyleSheet.create({
  title: { ...type.label, color: colors.ink },
  row: { flexDirection: 'row', alignItems: 'center', gap: space.sm, justifyContent: 'space-between' },
  who: { ...type.body, color: colors.ink, flex: 1 },
  error: { ...type.body, color: colors.danger },
});

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
import { Linking, Platform, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { useQuery } from '@tanstack/react-query';
import * as Location from 'expo-location';

import { api } from '../api/endpoints';
import { ApiError } from '../api/client';
import { athensTime } from '../design/format';
import { colors, space, type } from '../design/tokens';
import { Button, Card, Hint } from './ui';

const POLL_MS = 15_000;

export function LiveCard({
  tripId,
  now = () => Date.now(),
  carShared = false,
}: {
  tripId: string;
  now?: () => number;
  /** The car's position is already being shared by the drive's background
      task. Then this card only shows the others: a second, foreground upload
      would write fixes without accuracy over the task's, and stopping it
      would delete the car's position from under the drive. */
  carShared?: boolean;
}) {
  const { t } = useTranslation();
  const live = useQuery({
    queryKey: ['trips', tripId, 'live'],
    queryFn: () => api.live(tripId),
    refetchInterval: POLL_MS,
  });
  const [sharing, setSharing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const watch = useRef<Location.LocationSubscription | null>(null);
  // Bumped by every start and every stop. A callback, a permission prompt or a
  // watch that resolves under an older generation belongs to sharing that has
  // already ended, and does nothing — including the watch that finishes being
  // created after the screen was closed, which is removed on arrival.
  const generation = useRef(0);
  const starting = useRef(false);
  // The last upload still in flight. Stopping waits for it before deleting, or
  // a PUT that left before the DELETE can land after it and put the position
  // back — which is what QA saw when leaving the screen.
  const inFlight = useRef<Promise<unknown>>(Promise.resolve());
  const failures = useRef(0);

  const forget = () => {
    const done = inFlight.current;
    return done.catch(() => undefined).then(() => api.stopPosition(tripId));
  };

  const stop = async () => {
    generation.current++;
    watch.current?.remove();
    watch.current = null;
    setSharing(false);
    try {
      await forget();
    } catch {
      setError(t('live.stopFailed'));
    }
  };

  // Leaving the screen stops sharing, which is what the hint promised.
  useEffect(
    () => () => {
      generation.current++;
      if (watch.current) {
        watch.current.remove();
        watch.current = null;
        void forget().catch(() => undefined);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [tripId],
  );

  // «Φεύγω» hands the car's position to the drive's background task. A
  // foreground share already running stops here — and pending starts are
  // cancelled — but nothing is deleted: the position now belongs to the
  // drive, and the cleanup above has no watcher left to act on.
  useEffect(() => {
    if (!carShared) return;
    generation.current++;
    starting.current = false;
    if (watch.current) {
      watch.current.remove();
      watch.current = null;
    }
    setSharing(false);
  }, [carShared]);

  // A browser tab closing, or going to another page, unloads without
  // unmounting anything, so the cleanup above never runs. pagehide does, and a
  // keepalive request is allowed to finish after the page is gone.
  useEffect(() => {
    if (Platform.OS !== 'web' || typeof window === 'undefined' || !sharing) return;
    const onHide = () => void api.stopPosition(tripId, true).catch(() => undefined);
    window.addEventListener('pagehide', onHide);
    return () => window.removeEventListener('pagehide', onHide);
  }, [sharing, tripId]);

  const start = async () => {
    if (carShared || starting.current || watch.current) return;
    // Browsers only hand out a position to a secure page. Asking anyway ends
    // in a bare denial that reads like the user said no.
    if (Platform.OS === 'web' && typeof window !== 'undefined' && !window.isSecureContext) {
      setError(t('live.needsHttps'));
      return;
    }
    starting.current = true;
    setError(null);
    failures.current = 0;
    const mine = ++generation.current;
    try {
      const permission = await Location.requestForegroundPermissionsAsync();
      if (mine !== generation.current) return;
      if (!permission.granted) {
        setError(t('live.denied'));
        return;
      }
      const sub = await Location.watchPositionAsync(
        { accuracy: Location.Accuracy.Balanced, timeInterval: POLL_MS, distanceInterval: 30 },
        (pos) => {
          if (mine !== generation.current) return;
          // How good and when, so the server can order it against the
          // drive's own fixes rather than stamping it with its arrival.
          const put = api.putPosition(tripId, pos.coords.latitude, pos.coords.longitude, {
            accuracy_m: pos.coords.accuracy,
            observed_at: Number.isFinite(pos.timestamp) ? new Date(pos.timestamp).toISOString() : undefined,
          });
          inFlight.current = put;
          put.then(
            () => {
              failures.current = 0;
            },
            (e: unknown) => {
              if (mine !== generation.current) return;
              // The window closed, or the trip was called off: stop rather than
              // keep sending into a refusal. A closed window is said out loud,
              // or the button would just quietly come back.
              if (e instanceof ApiError && (e.code === 'not_sharing_now' || e.code === 'not_found')) {
                void stop();
                if (e.code === 'not_sharing_now') setError(t('live.notSharingNow'));
                return;
              }
              // Anything else may be a moment without signal. Three in a row is
              // not, and the screen must stop claiming the position is shared.
              failures.current += 1;
              if (failures.current >= 3) setError(t('live.sendFailed'));
            },
          );
        },
      );
      if (mine !== generation.current) {
        sub.remove();
        return;
      }
      watch.current = sub;
      setSharing(true);
    } catch {
      if (mine === generation.current) setError(t('common.error'));
    } finally {
      starting.current = false;
    }
  };

  if (live.isError && !live.data) {
    return (
      <Card testID="live-card">
        <Text style={styles.title}>{t('live.title')}</Text>
        <Hint testID="live-failed">{t('live.failed')}</Hint>
        <Button testID="live-retry" label={t('common.retry')} variant="secondary" onPress={() => void live.refetch()} />
      </Card>
    );
  }
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
            // The server leaves the caller out of the list, so while this
            // phone shares, an empty list means only me — not nobody.
            <Hint testID="live-nobody">{t(sharing || carShared ? 'live.onlyYou' : 'live.nobody')}</Hint>
          ) : (
            live.data.positions.map((p) => (
              <View key={p.user_id} testID={`live-${p.user_id}`} style={styles.row}>
                <Text style={styles.who}>{ageLine(p.name, p.updated_at, at, t)}</Text>
                <Button
                  testID={`live-map-${p.user_id}`}
                  label={t('live.openMap')}
                  variant="secondary"
                  onPress={() =>
                    void Linking.openURL(
                      `https://www.openstreetmap.org/?mlat=${p.lat}&mlon=${p.lon}#map=16/${p.lat}/${p.lon}`,
                    ).catch(() => setError(t('common.error')))
                  }
                />
              </View>
            ))
          )}
          {carShared ? (
            <Hint testID="live-car-shared">{t('drive.carShared')}</Hint>
          ) : sharing ? (
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

// "πριν 0′" reads like a clock that stopped. Under a minute is now.
function ageLine(name: string, updatedAt: string, at: number, t: TFunction) {
  const minutes = Math.max(0, Math.round((at - new Date(updatedAt).getTime()) / 60_000));
  return minutes === 0 ? t('live.now', { name }) : t('live.ago', { name, minutes });
}

const styles = StyleSheet.create({
  title: { ...type.label, color: colors.ink },
  row: { flexDirection: 'row', alignItems: 'center', gap: space.sm, justifyContent: 'space-between' },
  who: { ...type.body, color: colors.ink, flex: 1 },
  error: { ...type.body, color: colors.danger },
});

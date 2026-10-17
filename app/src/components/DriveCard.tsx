import { useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';

import { api, type TripDetail } from '../api/endpoints';
import { refusalText } from '../api/errorText';
import { athensTime } from '../design/format';
import { startDriveTracking } from '../drive/tracking';
import type { LonLat } from '../drive/maps';
import { colors, space, type } from '../design/tokens';
import { Button, Card, Chip, Hint } from './ui';

/**
 * «Φεύγω»: the driver says the minute they leave.
 *
 * A schedule says "between 07:00 and 07:30", and with only that the app could
 * not tell anybody when the car would reach them. Pinning the minute lets the
 * matcher ask the people on the road who will be there before the car — and
 * nobody else, because the driver never waits. See specs/DRIVING-NOW.md.
 *
 * The time is worked out when the button is pressed and sent as a time, not
 * as "in ten minutes", so a retry is the same request and passengers are told
 * once.
 */
export function DriveCard({
  trip,
  now = () => Date.now(),
  onDone,
  onTrackingWarning,
}: {
  trip: TripDetail;
  now?: () => number;
  onDone: () => void;
  /**
   * Where the position warning goes. The trip refetch that follows «Φεύγω»
   * replaces this card, so a warning kept in it would vanish; given this, the
   * screen shows it and the card does not.
   */
  onTrackingWarning?: (text: string | null) => void;
}) {
  const { t } = useTranslation();
  // Only the minutes the server would take: with somebody booked, not before
  // the window they agreed to, nor more than a quarter of an hour after it;
  // with nobody, an hour either side. Offering «Τώρα» only to refuse it is
  // the card arguing with itself.
  const allowed = WHEN.filter((m) => departureAllowed(trip, now() + m * 60_000));
  const [chosen, setInMin] = useState<number | null>(null);
  const inMin =
    chosen !== null && allowed.includes(chosen as (typeof WHEN)[number]) ? chosen : (allowed[0] ?? null);
  const [detour, setDetour] = useState<number>(nearestDetour(trip.detour_min ?? 5));
  const [error, setError] = useState<string | null>(null);

  const go = useMutation({
    mutationFn: (v: { departsAt: string; detour: number }) => api.startDrive(trip.id, v.departsAt, v.detour),
    onSuccess: async () => {
      setError(null);
      // Sharing the car starts with «Φεύγω», so that once it has left the
      // people ahead of it can be timed. Refused, the drive still stands; it
      // just takes nobody new once it is moving, and the driver is told why.
      // The drive has already started, so a task that will not start gets its
      // own message rather than «drive.failed».
      const started = await startDriveTracking(
        trip.id,
        { title: t('drive.trackTitle'), body: t('drive.trackBody') },
        trip.passengers.flatMap((p) => [p.pickup, p.dropoff]).filter((s): s is LonLat => !!s),
      ).catch(() => 'failed' as const);
      const warning =
        started === 'denied'
          ? t('drive.trackDenied')
          : started === 'unavailable'
            ? t('drive.trackWeb')
            : started === 'failed'
              ? t('drive.trackFailed')
              : null;
      if (onTrackingWarning) onTrackingWarning(warning);
      else if (warning) setError(warning);
      onDone();
    },
    onError: (e) => setError(refusalText(e) ?? t('drive.failed')),
  });

  const pinned = !!trip.drive_set_at;
  return (
    <Card testID="drive-card">
      <Text style={styles.title}>
        {pinned ? t('drive.leavingAt', { time: athensTime(trip.depart_earliest) }) : t('drive.title')}
      </Text>

      <Text style={styles.label}>{t('drive.when')}</Text>
      <View style={styles.row}>
        {WHEN.map((m) => (
          <Chip
            key={m}
            testID={`drive-when-${m}`}
            label={m === 0 ? t('drive.now') : t('drive.inMin', { n: m })}
            selected={inMin === m}
            onPress={() => setInMin(m)}
            disabled={go.isPending || !allowed.includes(m)}
          />
        ))}
      </View>
      {inMin === null ? (
        <Hint testID="drive-not-yet">
          {t('drive.bookedFrom', { time: athensTime(trip.scheduled_earliest ?? trip.depart_earliest) })}
        </Hint>
      ) : null}

      <Text style={styles.label}>{t('drive.detour')}</Text>
      <View style={styles.row}>
        {DETOURS.map((m) => (
          <Chip
            key={m}
            testID={`drive-detour-${m}`}
            label={m === 0 ? t('drive.onMyRoad') : t('drive.plusMin', { n: m })}
            selected={detour === m}
            onPress={() => setDetour(m)}
            disabled={go.isPending}
          />
        ))}
      </View>

      <Hint>{t('drive.hint')}</Hint>
      {error ? (
        <Text testID="drive-error" style={styles.error}>
          {error}
        </Text>
      ) : null}
      <Button
        testID="drive-go"
        label={pinned ? t('drive.change') : t('drive.go')}
        disabled={go.isPending || inMin === null}
        onPress={() =>
          inMin !== null &&
          go.mutate({
            departsAt: new Date(Math.floor((now() + inMin * 60_000) / 60_000) * 60_000).toISOString(),
            detour,
          })
        }
      />
    </Card>
  );
}

const WHEN = [0, 5, 10, 15] as const;
const DETOURS = [0, 3, 5, 10] as const;
const HOUR = 60 * 60_000;

/** The server's bounds on a departure, measured against the published window. */
function departureAllowed(trip: TripDetail, at: number): boolean {
  const opens = Date.parse(trip.scheduled_earliest ?? trip.depart_earliest);
  const closes = Date.parse(trip.scheduled_latest ?? trip.depart_latest);
  const minute = Math.floor(at / 60_000) * 60_000;
  return trip.passengers.length > 0
    ? minute >= opens && minute <= closes + QUARTER
    : minute >= opens - HOUR && minute <= closes + HOUR;
}

const QUARTER = 15 * 60_000;

function nearestDetour(min: number): number {
  return DETOURS.reduce((best, d) => (Math.abs(d - min) < Math.abs(best - min) ? d : best), 5);
}

/**
 * Whether the card belongs on the screen: the driver's own trip, still running,
 * from an hour before its published window to an hour after — the same bounds
 * the server holds an empty car to — and, once a minute is pinned, until it.
 */
export function driveCardShown(trip: TripDetail, at: number): boolean {
  if (trip.role !== 'driver' || trip.status !== 'scheduled') return false;
  if (trip.drive_ended_at) return false;
  if (trip.drive_set_at) return at < Date.parse(trip.depart_earliest);
  const opens = Date.parse(trip.scheduled_earliest ?? trip.depart_earliest);
  const closes = Date.parse(trip.scheduled_latest ?? trip.depart_latest);
  return at >= opens - HOUR && at <= closes + HOUR;
}

const styles = StyleSheet.create({
  title: { ...type.title, color: colors.ink },
  label: { ...type.label, color: colors.muted, marginTop: space.md },
  row: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: space.sm,
    marginTop: space.xs,
  },
  error: { ...type.body, color: colors.danger, marginTop: space.sm },
});

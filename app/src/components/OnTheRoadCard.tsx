import { useEffect, useState } from 'react';
import { Linking, StyleSheet, Switch, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';

import { api, type TripDetail } from '../api/endpoints';
import { refusalText } from '../api/errorText';
import { athensTime } from '../design/format';
import { colors, space, type } from '../design/tokens';
import { mapsUrl, remainingStops, type LonLat } from '../drive/maps';
import { decodeRoute } from './RouteGlyph';
import { getMapsApp } from '../drive/mapsApp';
import { setDriveStops, stopDriveTracking, visitedStops } from '../drive/tracking';
import { Badge, Button, Card, Chip, Hint } from './ui';

/**
 * The driver's trip once the car has left: a screen glanced at in a holder.
 *
 * Every control is one tap — take people or not, how far off the road, the
 * maps app for a pickup, «Τέλος» — because nothing here should need reading
 * while the car moves. Offers themselves arrive as notifications with their
 * own buttons; this is where the driver changes their mind about them.
 */
export function OnTheRoadCard({
  trip,
  now = () => Date.now(),
  onChanged,
}: {
  trip: TripDetail;
  now?: () => number;
  onChanged: () => void;
}) {
  const { t } = useTranslation();
  const [error, setError] = useState<string | null>(null);
  const failed = (e: unknown) => setError(refusalText(e) ?? t('drive.failed'));

  const adjust = useMutation({
    mutationFn: (body: { offering?: boolean; detour_min?: number }) => api.adjustDrive(trip.id, body),
    onSuccess: () => {
      setError(null);
      onChanged();
    },
    onError: failed,
  });
  const end = useMutation({
    mutationFn: async () => {
      await api.endDrive(trip.id);
      await stopDriveTracking(trip.id);
    },
    onSuccess: onChanged,
    onError: failed,
  });

  const offering = !!trip.offers_until && Date.parse(trip.offers_until) > now();
  const destination = trip.destination ?? null;
  // Every stop not yet visited — pickups and drop-offs, in road order — then
  // the end of the road. One button: in a car there is no choosing.
  const stops = trip.passengers.flatMap((p) => [p.pickup, p.dropoff]).filter((s): s is LonLat => !!s);
  useEffect(() => {
    void setDriveStops(stops);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(stops)]);
  const navigate = async () => {
    if (!destination) return;
    const left = remainingStops(decodeRoute(trip.route_polyline), trip.passengers, await visitedStops());
    await Linking.openURL(mapsUrl(await getMapsApp(), { stops: left, destination })).catch(() => undefined);
  };
  const busy = adjust.isPending || end.isPending;

  return (
    <Card testID="road-card">
      <Text style={styles.title}>{t('drive.driving')}</Text>

      <View style={styles.toggle}>
        <Text style={styles.toggleLabel}>
          {offering && trip.offers_until
            ? t('drive.offeringUntil', { time: athensTime(trip.offers_until) })
            : t('drive.notOffering')}
        </Text>
        <Switch
          testID="road-offering"
          accessibilityLabel={t('drive.offering')}
          value={offering}
          disabled={busy}
          onValueChange={(on) => adjust.mutate({ offering: on })}
          trackColor={{ true: colors.ink, false: colors.line }}
        />
      </View>

      {offering ? <Text style={styles.label}>{t('drive.detour')}</Text> : null}
      {offering ? (
        <View style={styles.row}>
          {DETOURS.map((m) => (
            <Chip
              key={m}
              testID={`road-detour-${m}`}
              label={m === 0 ? t('drive.onMyRoad') : t('drive.plusMin', { n: m })}
              selected={nearestDetour(trip.detour_min ?? 5) === m}
              onPress={() => adjust.mutate({ detour_min: m })}
              disabled={busy}
              fit
            />
          ))}
        </View>
      ) : null}

      {trip.passengers.map((p) => (
        <View key={p.id} testID={`road-passenger-${p.id}`} style={styles.passenger}>
          <Text style={styles.name}>{p.name}</Text>
          {p.arrived_at ? <Badge label={t('drive.atPickup')} tone="accent" /> : null}
        </View>
      ))}

      {destination ? (
        <Button testID="road-navigate" label={t('drive.navigate')} onPress={() => void navigate()} />
      ) : null}

      {error ? <Hint testID="road-error">{error}</Hint> : null}
      <Button
        testID="road-end"
        label={t('drive.end')}
        variant="danger"
        disabled={busy}
        onPress={() => end.mutate()}
      />
    </Card>
  );
}

const DETOURS = [0, 3, 5, 10] as const;

function nearestDetour(min: number): number {
  return DETOURS.reduce((best, d) => (Math.abs(d - min) < Math.abs(best - min) ? d : best), 5);
}

/** Whether the driver is on the road: the pinned minute has passed and they have not said «Τέλος». */
export function onTheRoad(trip: TripDetail, at: number): boolean {
  return (
    trip.role === 'driver' &&
    trip.status === 'scheduled' &&
    !!trip.drive_set_at &&
    !trip.drive_ended_at &&
    at >= Date.parse(trip.depart_earliest)
  );
}

const styles = StyleSheet.create({
  title: { ...type.title, color: colors.ink },
  toggle: { flexDirection: 'row', alignItems: 'center', gap: space.md, marginTop: space.md },
  toggleLabel: { ...type.body, color: colors.ink, flex: 1 },
  label: { ...type.label, color: colors.muted, marginTop: space.md },
  row: { flexDirection: 'row', flexWrap: 'wrap', gap: space.sm, marginTop: space.sm },
  passenger: {
    flexDirection: 'row',
    alignItems: 'center',
    flexWrap: 'wrap',
    gap: space.sm,
    marginTop: space.md,
  },
  name: { ...type.body, color: colors.ink, flex: 1 },
});

import { useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, type PastTrip } from '../api/endpoints';
import { ApiError } from '../api/client';
import { Button, Card } from './ui';
import { colors, radius, space, type } from '../design/tokens';
import { athensDay } from '../design/format';

/**
 * The rides already taken, each with one button.
 *
 * "Ξανά" does not book anything: it asks the same driver about their next
 * running of that route, and they answer as they would any other request. The
 * standing switch on the trip screen is for the commute; this is for the ride
 * that happens when it happens.
 */
export function PastTrips() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [notice, setNotice] = useState<string | null>(null);

  const past = useQuery({ queryKey: ['trips', 'past'], queryFn: () => api.pastTrips() });

  const again = useMutation({
    mutationFn: (bookingId: string) => api.again(bookingId),
    onSuccess: (res) => {
      setNotice(res.asking ? t('home.againAsked') : t('home.againNone'));
      void queryClient.invalidateQueries({ queryKey: ['trips'] });
    },
    onError: (e) =>
      setNotice(
        e instanceof ApiError && e.code === 'nothing_to_repeat'
          ? t('home.againNone')
          : t('common.error'),
      ),
  });

  const trips = past.data?.trips ?? [];
  if (trips.length === 0) return null;

  return (
    <View testID="past-trips" style={styles.wrap}>
      <Text style={styles.heading}>{t('home.past')}</Text>

      {notice ? (
        <Text testID="past-notice" style={styles.notice}>
          {notice}
        </Text>
      ) : null}

      {trips.map((trip: PastTrip) => (
        <Card key={trip.booking_id}>
          <View style={styles.row}>
            <View style={styles.text}>
              <Text style={styles.route}>
                {trip.origin_label} → {trip.destination_label}
              </Text>
              <Text style={styles.meta}>
                {athensDay(trip.depart_earliest)} · {trip.driver_name}
                {trip.price_eur > 0 ? ` · ${trip.price_eur.toFixed(2)} €` : ''}
              </Text>
            </View>
            <Button
              testID={`again-${trip.booking_id}`}
              label={t('home.again')}
              variant="secondary"
              disabled={again.isPending}
              onPress={() => again.mutate(trip.booking_id)}
            />
          </View>
        </Card>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: space.sm, marginTop: space.xl },
  heading: { ...type.title, color: colors.ink, marginBottom: space.xs },
  row: { flexDirection: 'row', alignItems: 'center', gap: space.md },
  text: { flex: 1 },
  route: { ...type.body, color: colors.ink, fontWeight: '600' },
  meta: { ...type.caption, color: colors.muted, marginTop: 2 },
  notice: {
    ...type.caption,
    color: colors.ink,
    backgroundColor: colors.sunken,
    borderRadius: radius.inner,
    padding: space.sm,
  },
});

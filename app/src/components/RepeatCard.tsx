import { useState } from 'react';
import { StyleSheet, Switch, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, type Arrangement, type TripDetail } from '../api/endpoints';
import { ApiError } from '../api/client';
import { Button, Card } from './ui';
import { colors, radius, space, type } from '../design/tokens';
import { repeatUntil } from '../design/format';

/**
 * The switch that records an arrangement two people already made.
 *
 * It is one control with several truths behind it, so the copy states which one
 * is showing rather than leaving a half-lit toggle to imply a seat that is not
 * actually held: waiting on the driver, holding N weeks, or turned down.
 */
export function RepeatCard({ trip }: { trip: TripDetail }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [notice, setNotice] = useState<string | null>(null);

  const arrangements = useQuery({
    queryKey: ['arrangements'],
    queryFn: () => api.arrangements(),
  });

  const mine: Arrangement | undefined = arrangements.data?.arrangements.find(
    (a) =>
      a.template_id === trip.template_id &&
      a.weekday === trip.weekday &&
      (a.status === 'pending' || a.status === 'active'),
  );
  const declined = arrangements.data?.arrangements.find(
    (a) => a.template_id === trip.template_id && a.weekday === trip.weekday && a.status === 'declined',
  );

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['arrangements'] });
    void queryClient.invalidateQueries({ queryKey: ['trips'] });
  };
  const fail = (e: unknown) => {
    if (e instanceof ApiError && e.code === 'week_full') {
      const dates = (e.details?.dates as string[] | undefined) ?? [];
      setNotice(t('trip.repeatWeekFull', { dates: dates.join(', ') }));
      return;
    }
    setNotice(t('common.error'));
  };

  const ask = useMutation({
    mutationFn: () => api.repeatBooking(trip.booking_id as string),
    onSuccess: refresh,
    onError: fail,
  });
  const approve = useMutation({
    mutationFn: () => api.approveArrangement(mine!.id),
    onSuccess: refresh,
    onError: fail,
  });
  const decline = useMutation({
    mutationFn: () => api.declineArrangement(mine!.id),
    onSuccess: refresh,
    onError: fail,
  });
  const end = useMutation({
    mutationFn: () => api.cancelArrangement(mine!.id),
    onSuccess: refresh,
    onError: fail,
  });

  const day = t(`weekdays.${trip.weekday}`);
  const isDriver = trip.role === 'driver';

  // The driver has no booking of their own, so they cannot start an
  // arrangement — only answer one.
  // A one-off route does not run again, so there is nothing to switch on.
  if (trip.repeats === false) return null;
  if (isDriver && !mine) return null;
  if (!isDriver && !trip.booking_id) return null;

  const busy = ask.isPending || approve.isPending || decline.isPending || end.isPending;

  return (
    <Card>
      <View style={styles.row}>
        <View style={styles.text}>
          <Text style={styles.title}>
            {isDriver && mine?.status === 'pending'
              ? t('trip.repeatPendingDriver', { name: mine.passenger_name, day })
              : t('trip.repeatTitle', { day })}
          </Text>
          <Text testID="repeat-state" style={styles.hint}>
            {mine?.status === 'active'
              ? t('trip.repeatActive', { count: mine.weeks_booked })
              : mine?.status === 'pending'
                ? t('trip.repeatPending')
                : declined
                  ? t('trip.repeatDeclined')
                  : t('trip.repeatHint', { until: repeatUntil(new Date()) })}
          </Text>
        </View>

        {/* The passenger's switch starts it and ends it. The driver answers
            with words, because approving is a decision and not a preference. */}
        {!isDriver ? (
          <Switch
            testID="repeat-switch"
            disabled={busy}
            value={Boolean(mine)}
            onValueChange={(on) => (on ? ask.mutate() : end.mutate())}
            trackColor={{ true: colors.ink, false: colors.line }}
          />
        ) : null}
      </View>

      {isDriver && mine?.status === 'pending' ? (
        <View style={styles.actions}>
          <Button testID="repeat-approve" label={t('trip.repeatApprove')} onPress={() => approve.mutate()} disabled={busy} />
          <Button
            testID="repeat-decline"
            label={t('trip.repeatDecline')}
            variant="secondary"
            onPress={() => decline.mutate()}
            disabled={busy}
          />
        </View>
      ) : null}

      {isDriver && mine?.status === 'active' ? (
        <View style={styles.actions}>
          <Button testID="repeat-end" label={t('trip.repeatEnd')} variant="secondary" onPress={() => end.mutate()} disabled={busy} />
        </View>
      ) : null}

      {notice ? (
        <Text testID="repeat-notice" style={styles.notice}>
          {notice}
        </Text>
      ) : null}
    </Card>
  );
}

const styles = StyleSheet.create({
  row: { flexDirection: 'row', alignItems: 'center', gap: space.md },
  text: { flex: 1 },
  title: { ...type.label, fontSize: 16, color: colors.ink },
  hint: { ...type.caption, color: colors.muted, marginTop: 2 },
  actions: { flexDirection: 'row', gap: space.sm, marginTop: space.md },
  notice: {
    ...type.caption,
    color: colors.ink,
    backgroundColor: colors.surface,
    borderRadius: radius.control,
    padding: space.sm,
    marginTop: space.sm,
  },
});

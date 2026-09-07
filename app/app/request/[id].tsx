import { useLocalSearchParams, useRouter } from 'expo-router';
import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { api } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { requestProgress, useOpenRequests } from '../../src/components/OpenRequests';
import { Button, Card, Hint, Screen, Title } from '../../src/components/ui';
import { colors, space, type } from '../../src/design/tokens';
import { athensDay, athensTime } from '../../src/design/format';

export default function RequestScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id: string }>();
  const queryClient = useQueryClient();
  const [outcome, setOutcome] = useState<string | null>(null);
  // A driver took it. Different from an ordinary failure: there is nothing left
  // to retry here, and there is now a trip to go and look at.
  const [matched, setMatched] = useState(false);

  // The same list, and the same sentence, as the card on the home screen that
  // leads here. Without it this screen was a pulsing dot over a raw uuid — less
  // than the card said, which made opening it a step backwards.
  //
  // It polls, so the count of drivers who could not climbs while somebody sits
  // here. Not found means matched, cancelled, expired, or simply not loaded
  // yet, and none of those is worth guessing at out loud.
  const requests = useOpenRequests();
  const mine = requests.data?.requests.find((q) => q.id === id) ?? null;

  const cancel = useMutation({
    mutationFn: () => api.cancelRequest(id),
    onSuccess: () => {
      // The home screen lists what is still being waited on, and this is not
      // one of those any more.
      void queryClient.invalidateQueries({ queryKey: ['requests'] });
      router.replace('/(tabs)');
    },
    // Leaving the screen is what tells somebody they are not going, so it only
    // happens on an actual cancellation. This screen is open exactly while
    // drivers are being asked, so a driver accepting mid-press is ordinary
    // rather than rare, and the seat is then held in their name.
    onError: (e: unknown) => {
      if (e instanceof ApiError && e.code === 'already_matched') {
        setMatched(true);
        return setOutcome(t('request.alreadyMatched'));
      }
      // Anything else may well work on a second press, so the button stays.
      setOutcome(t('common.error'));
    },
  });

  return (
    <Screen>
      <Title>{t('search.asking')}</Title>

      <Card>
        {/* Asking is sequential and one driver at a time, so waiting is the
            normal state here rather than a failure. */}
        <View style={styles.pulse}>
          <View style={styles.ring} />
          <View style={styles.core} />
        </View>
        {mine ? (
          <>
            <Text testID="request-route" style={styles.route} numberOfLines={1}>
              {mine.origin_label} → {mine.destination_label}
            </Text>
            <Text style={styles.when}>
              {athensDay(mine.window_start)} · {athensTime(mine.window_start)}–
              {athensTime(mine.window_end)}
            </Text>
            <Text testID="request-progress" style={styles.progress}>
              {requestProgress(mine, t)}
            </Text>
          </>
        ) : (
          <Hint>{t('search.noDrivers')}</Hint>
        )}
      </Card>

      {outcome ? (
        <Text testID="request-outcome" style={styles.outcome}>
          {outcome}
        </Text>
      ) : null}

      <View style={styles.spacer} />

      {matched ? (
        <Button
          testID="request-go-to-trips"
          label={t('request.seeMyTrips')}
          onPress={() => router.replace('/(tabs)')}
        />
      ) : (
        <Button
          testID="cancel-request"
          label={t('common.cancel')}
          variant="secondary"
          onPress={() => cancel.mutate()}
          disabled={cancel.isPending}
        />
      )}
    </Screen>
  );
}

const styles = StyleSheet.create({
  pulse: { alignItems: 'center', justifyContent: 'center', height: 96 },
  ring: {
    position: 'absolute',
    width: 88,
    height: 88,
    borderRadius: 44,
    borderWidth: 2,
    borderColor: colors.accentSoft,
  },
  core: { width: 18, height: 18, borderRadius: 9, backgroundColor: colors.accent },
  route: { ...type.body, color: colors.ink, textAlign: 'center' },
  when: { ...type.caption, color: colors.muted, marginTop: 2, textAlign: 'center' },
  progress: {
    ...type.caption,
    color: colors.accent,
    fontWeight: '700',
    marginTop: space.xs,
    textAlign: 'center',
  },
  outcome: { ...type.body, color: colors.danger },
  spacer: { flex: 1 },
});

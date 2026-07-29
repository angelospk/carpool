// The ask screen is the heart of the driver experience: it opens from a push
// notification and must answer "is this worth it?" in one glance and one
// round trip.

import { useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { api } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { Button, Screen, Title, colors } from '../../src/components/ui';

export default function AskScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const queryClient = useQueryClient();
  const { id, detour_min, detour_km, price } = useLocalSearchParams<{
    id: string;
    detour_min?: string;
    detour_km?: string;
    price?: string;
  }>();
  const [outcome, setOutcome] = useState<string | null>(null);

  const answered = () => {
    void queryClient.invalidateQueries({ queryKey: ['trips'] });
    router.replace('/(tabs)');
  };

  // A stale tap is normal: the push may be minutes old and the ask may have
  // expired or been taken. Each case gets its own message rather than a generic
  // failure.
  const onError = (e: unknown) => {
    if (!(e instanceof ApiError)) return setOutcome(t('common.error'));
    switch (e.code) {
      case 'ask_expired':
        return setOutcome(t('ask.expired'));
      case 'no_seats':
        return setOutcome(t('ask.noSeats'));
      case 'ask_closed':
        return setOutcome(t('ask.closed'));
      default:
        return setOutcome(t('common.error'));
    }
  };

  const accept = useMutation({
    mutationFn: () => api.acceptAsk(id),
    onSuccess: answered,
    onError,
  });
  const decline = useMutation({
    mutationFn: () => api.declineAsk(id),
    onSuccess: answered,
    onError,
  });

  const busy = accept.isPending || decline.isPending;

  return (
    <Screen>
      <Title>{t('ask.title')}</Title>

      <View style={styles.card}>
        <Text testID="ask-detour" style={styles.detour}>
          {t('ask.detour', {
            minutes: Math.round(Number(detour_min ?? 0)),
            km: Number(detour_km ?? 0).toFixed(1),
          })}
        </Text>
        {price ? (
          <Text style={styles.price}>
            {Number(price).toFixed(2)} € <Text style={styles.suggested}>{t('ask.suggested')}</Text>
          </Text>
        ) : null}
      </View>

      {outcome ? (
        <Text testID="ask-outcome" style={styles.outcome}>
          {outcome}
        </Text>
      ) : null}

      <Button
        testID="ask-accept"
        label={t('ask.accept')}
        onPress={() => accept.mutate()}
        disabled={busy || !!outcome}
      />
      <Button
        testID="ask-decline"
        label={t('ask.decline')}
        variant="secondary"
        onPress={() => decline.mutate()}
        disabled={busy || !!outcome}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  card: {
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: 16,
    padding: 20,
    gap: 8,
  },
  detour: { fontSize: 30, fontWeight: '700', color: colors.text },
  price: { fontSize: 20, color: colors.text },
  // Never "fare": the app suggests a contribution, it does not charge one.
  suggested: { fontSize: 14, color: colors.muted },
  outcome: { fontSize: 16, color: colors.danger },
});

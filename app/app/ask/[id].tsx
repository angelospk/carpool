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
import { Badge, Button, Card, Screen, Title } from '../../src/components/ui';
import { colors, space, type } from '../../src/design/tokens';

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

      {/* The detour is the decision. It gets the size, and everything else
          arranges itself around it. */}
      <Card tone="accent">
        <Badge label={t('ask.waiting')} tone="accent" />
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
      </Card>

      {outcome ? (
        <Text testID="ask-outcome" style={styles.outcome}>
          {outcome}
        </Text>
      ) : null}

      <View style={styles.spacer} />

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
  detour: { ...type.display, fontSize: 32, color: colors.ink },
  price: { ...type.title, color: colors.ink },
  // Never "fare": the app suggests a contribution, it does not charge one.
  suggested: { ...type.caption, color: colors.muted },
  outcome: { ...type.body, color: colors.danger },
  spacer: { flex: 1 },
});

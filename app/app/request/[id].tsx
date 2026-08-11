import { useLocalSearchParams, useRouter } from 'expo-router';
import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';
import { useState } from 'react';

import { api } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { Button, Card, Hint, Screen, Title } from '../../src/components/ui';
import { colors, space, type } from '../../src/design/tokens';

export default function RequestScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id: string }>();
  const [outcome, setOutcome] = useState<string | null>(null);
  // A driver took it. Different from an ordinary failure: there is nothing left
  // to retry here, and there is now a trip to go and look at.
  const [matched, setMatched] = useState(false);

  const cancel = useMutation({
    mutationFn: () => api.cancelRequest(id),
    onSuccess: () => router.replace('/(tabs)'),
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
        <Hint>{t('search.noDrivers')}</Hint>
        <Text style={styles.id}>{id}</Text>
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
  id: { ...type.caption, fontSize: 11, color: colors.muted },
  outcome: { ...type.body, color: colors.danger },
  spacer: { flex: 1 },
});

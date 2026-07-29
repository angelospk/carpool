import { useState } from 'react';
import { FlatList, StyleSheet, Text, TextInput, View } from 'react-native';
import { useLocalSearchParams } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { useSession } from '../../src/state/session';
import { AsyncState, Button, Screen, Title, colors } from '../../src/components/ui';

export default function TripScreen() {
  const { t } = useTranslation();
  const { id } = useLocalSearchParams<{ id: string }>();
  const queryClient = useQueryClient();
  const me = useSession((s) => s.me);
  const [draft, setDraft] = useState('');
  const [notice, setNotice] = useState<string | null>(null);

  const trip = useQuery({ queryKey: ['trips', id], queryFn: () => api.trip(id) });
  const messages = useQuery({
    queryKey: ['trips', id, 'messages'],
    queryFn: () => api.messages(id),
    // Chat is polled while the screen is open. The websocket is the fast path;
    // this is the one that always works.
    refetchInterval: 10_000,
  });

  const send = useMutation({
    mutationFn: (body: string) => api.postMessage(id, body),
    onSuccess: () => {
      setDraft('');
      void queryClient.invalidateQueries({ queryKey: ['trips', id, 'messages'] });
    },
  });

  const confirm = useMutation({
    mutationFn: () => api.confirmTrip(id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['trips', id] }),
    onError: (e) =>
      setNotice(
        e instanceof ApiError && e.code === 'not_departed'
          ? t('trip.notDeparted')
          : t('common.error'),
      ),
  });

  const rate = useMutation({
    mutationFn: (stars: number) =>
      api.rateTrip(id, stars, trip.data?.role === 'driver' ? trip.data.passengers[0]?.id : undefined),
    onSuccess: () => setNotice(t('trip.rated')),
    onError: () => setNotice(t('common.error')),
  });

  return (
    <Screen>
      <AsyncState
        loading={trip.isLoading}
        error={trip.error}
        onRetry={() => void trip.refetch()}
      >
        {trip.data ? (
          <>
            <Title>
              {trip.data.origin_label} → {trip.data.destination_label}
            </Title>

            <View style={styles.card}>
              <Text style={styles.label}>{t('trip.driver')}</Text>
              <Text style={styles.value}>
                {trip.data.driver.name}
                {trip.data.driver.rating ? ` · ★ ${trip.data.driver.rating}` : ''}
              </Text>
              <Text style={styles.muted}>
                {trip.data.driver.car_model}
                {trip.data.driver.car_color ? ` · ${trip.data.driver.car_color}` : ''}
              </Text>
              {/* The plate only exists in this payload once a booking is made. */}
              {trip.data.driver.car_plate ? (
                <Text testID="car-plate" style={styles.plate}>
                  {t('trip.plate')}: {trip.data.driver.car_plate}
                </Text>
              ) : null}
            </View>

            {trip.data.price_eur > 0 ? (
              <Text style={styles.price}>
                {trip.data.price_eur.toFixed(2)} €{' '}
                <Text style={styles.muted}>{t('ask.suggested')}</Text>
              </Text>
            ) : null}

            {notice ? (
              <Text testID="trip-notice" style={styles.notice}>
                {notice}
              </Text>
            ) : null}

            <Button
              testID="trip-confirm"
              label={t('trip.confirm')}
              onPress={() => confirm.mutate()}
              disabled={confirm.isPending}
            />
            <Button
              testID="trip-rate"
              label={`${t('trip.rate')} ★★★★★`}
              variant="secondary"
              onPress={() => rate.mutate(5)}
              disabled={rate.isPending}
            />

            <Text style={styles.label}>{t('trip.chat')}</Text>
            <FlatList
              testID="message-list"
              style={styles.messages}
              data={messages.data?.messages ?? []}
              keyExtractor={(m) => m.id}
              renderItem={({ item }) => (
                <View style={item.sender_id === me?.id ? styles.mine : styles.theirs}>
                  <Text style={styles.muted}>{item.sender_name}</Text>
                  <Text style={styles.value}>{item.body}</Text>
                </View>
              )}
            />
            <View style={styles.composer}>
              <TextInput
                testID="message-input"
                style={[styles.input, styles.composerInput]}
                value={draft}
                onChangeText={setDraft}
                placeholder={t('trip.messagePlaceholder')}
              />
              <Button
                testID="message-send"
                label={t('trip.send')}
                onPress={() => send.mutate(draft)}
                disabled={!draft.trim() || send.isPending}
              />
            </View>
          </>
        ) : null}
      </AsyncState>
    </Screen>
  );
}

const styles = StyleSheet.create({
  card: { borderWidth: 1, borderColor: colors.border, borderRadius: 14, padding: 16, gap: 3 },
  label: { fontSize: 13, color: colors.muted },
  value: { fontSize: 16, color: colors.text },
  muted: { fontSize: 13, color: colors.muted },
  plate: { fontSize: 15, fontWeight: '600', color: colors.text, marginTop: 4 },
  price: { fontSize: 18, color: colors.text },
  notice: { fontSize: 15, color: colors.danger },
  messages: { flex: 1 },
  mine: { alignSelf: 'flex-end', padding: 8 },
  theirs: { alignSelf: 'flex-start', padding: 8 },
  composer: { flexDirection: 'row', gap: 8, alignItems: 'center' },
  composerInput: { flex: 1 },
  input: { borderWidth: 1, borderColor: colors.border, borderRadius: 12, padding: 12, fontSize: 16 },
});

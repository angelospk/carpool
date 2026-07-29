import { useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';

import { api } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { Button, Card, Field, Hint, Screen, Stepper, Title } from '../../src/components/ui';
import { colors, space, type } from '../../src/design/tokens';
import { parsePin, type Pin } from '../../src/design/pin';

export default function SearchScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const [fromText, setFromText] = useState('');
  const [toText, setToText] = useState('');
  const [seats, setSeats] = useState(1);
  const [outcome, setOutcome] = useState<string | null>(null);

  const from = parsePin(fromText);
  const to = parsePin(toText);

  const create = useMutation({
    mutationFn: () => {
      // A two-hour window from now is the common case; the picker refines it.
      const start = new Date();
      const end = new Date(start.getTime() + 2 * 60 * 60 * 1000);
      return api.createRequest({
        origin_lon: (from as Pin).lon,
        origin_lat: (from as Pin).lat,
        destination_lon: (to as Pin).lon,
        destination_lat: (to as Pin).lat,
        origin_label: (from as Pin).label,
        destination_label: (to as Pin).label,
        window_start: start.toISOString(),
        window_end: end.toISOString(),
        seats,
      });
    },
    onSuccess: (res) => {
      // No driver yet is a normal outcome, not an error: the request stays open
      // and the server asks someone when a route appears.
      setOutcome(res.asking ? t('search.asking') : t('search.noDrivers'));
      router.push(`/request/${res.request_id}`);
    },
    onError: (e) =>
      setOutcome(
        e instanceof ApiError && e.code === 'outside_region' ? e.message : t('common.error'),
      ),
  });

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.body} showsVerticalScrollIndicator={false}>
        <Title>{t('search.title')}</Title>

        <Card>
          <View style={styles.pins}>
            <View style={styles.rail}>
              <View style={[styles.dot, from && styles.dotSet]} />
              <View style={styles.line} />
              <View style={[styles.dot, to && styles.dotSet]} />
            </View>
            <View style={styles.fields}>
              <Field
                testID="from-input"
                label={t('search.from')}
                value={fromText}
                onChangeText={setFromText}
                placeholder="25.87, 40.85 Αλεξανδρούπολη"
                autoCapitalize="none"
                valid={!!from}
              />
              <Field
                testID="to-input"
                label={t('search.to')}
                value={toText}
                onChangeText={setToText}
                placeholder="25.40, 41.12 Κομοτηνή"
                autoCapitalize="none"
                valid={!!to}
              />
            </View>
          </View>

          <View style={styles.seats}>
            <Text style={styles.seatsLabel}>{t('search.seats')}</Text>
            <Stepper testID="seats" value={seats} onChange={setSeats} min={1} max={4} />
          </View>
        </Card>

        {outcome ? <Hint>{outcome}</Hint> : null}

        <Button
          testID="search-submit"
          label={t('search.post')}
          onPress={() => create.mutate()}
          disabled={!from || !to || create.isPending}
        />
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { gap: space.lg, paddingBottom: 140 },
  pins: { flexDirection: 'row', gap: space.md },
  rail: { alignItems: 'center', paddingVertical: 26 },
  dot: { width: 12, height: 12, borderRadius: 6, borderWidth: 2.5, borderColor: colors.line },
  dotSet: { borderColor: colors.primary },
  line: { flex: 1, width: 2, backgroundColor: colors.line, minHeight: 20 },
  fields: { flex: 1, gap: space.sm },
  seats: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  seatsLabel: { ...type.body, color: colors.ink },
});

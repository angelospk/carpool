import { useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';

import { api } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import {
  Button,
  Card,
  Chip,
  Field,
  Hint,
  Screen,
  SectionHeader,
  Stepper,
  Title,
} from '../../src/components/ui';
import { colors, space, type } from '../../src/design/tokens';
import { type Pin } from '../../src/design/pin';
import { PlaceField } from '../../src/components/PlaceField';

const WEEKDAYS = [
  { iso: 1, label: 'Δε' },
  { iso: 2, label: 'Τρ' },
  { iso: 3, label: 'Τε' },
  { iso: 4, label: 'Πε' },
  { iso: 5, label: 'Πα' },
  { iso: 6, label: 'Σα' },
  { iso: 7, label: 'Κυ' },
];

export default function PublishScreen() {
  const { t } = useTranslation();
  // The wizard defaults aggressively — the target is a recurring route
  // published in under 90 seconds, so only the two pins are truly required.
  const [from, setFrom] = useState<Pin | null>(null);
  const [to, setTo] = useState<Pin | null>(null);
  const [weekdays, setWeekdays] = useState<number[]>([1, 2, 3, 4, 5]);
  const [earliest, setEarliest] = useState('07:30');
  const [latest, setLatest] = useState('08:00');
  const [seats, setSeats] = useState(3);
  const [outcome, setOutcome] = useState<string | null>(null);


  const publish = useMutation({
    mutationFn: () =>
      api.publishRoute({
        origin_lon: (from as Pin).lon,
        origin_lat: (from as Pin).lat,
        destination_lon: (to as Pin).lon,
        destination_lat: (to as Pin).lat,
        origin_label: (from as Pin).label,
        destination_label: (to as Pin).label,
        seats,
        kind: 'recurring',
        weekdays,
        depart_earliest: earliest,
        depart_latest: latest,
      }),
    onSuccess: (r) => setOutcome(`${t('publish.published')} · ${r.route_km.toFixed(1)} km`),
    onError: (e) =>
      setOutcome(
        e instanceof ApiError && e.code === 'no_driver_profile' ? e.message : t('common.error'),
      ),
  });

  const toggle = (iso: number) =>
    setWeekdays((d) => (d.includes(iso) ? d.filter((x) => x !== iso) : [...d, iso].sort()));

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.body} showsVerticalScrollIndicator={false}>
        <Title>{t('publish.title')}</Title>

        <SectionHeader title={t('publish.route')} />
        <Card>
          <PlaceField
            testID="publish-origin"
            label={t('search.from')}
            value={from}
            onChange={setFrom}
            placeholder={t('search.placeholder')}
          />
          <PlaceField
            testID="publish-destination"
            label={t('search.to')}
            value={to}
            onChange={setTo}
            placeholder={t('search.placeholder')}
          />
        </Card>

        <SectionHeader title={t('publish.schedule')} />
        <Card>
          <Text style={styles.label}>{t('publish.recurring')}</Text>
          <View style={styles.days}>
            {WEEKDAYS.map((d) => (
              <Chip
                key={d.iso}
                testID={`weekday-${d.iso}`}
                label={d.label}
                selected={weekdays.includes(d.iso)}
                onPress={() => toggle(d.iso)}
              />
            ))}
          </View>

          <Text style={styles.label}>{t('publish.departWindow')}</Text>
          <View style={styles.times}>
            <Field
              testID="depart-earliest"
              label={t('publish.from')}
              value={earliest}
              onChangeText={setEarliest}
              compact
            />
            <Field
              testID="depart-latest"
              label={t('publish.until')}
              value={latest}
              onChangeText={setLatest}
              compact
            />
          </View>

          <View style={styles.seats}>
            <Text style={styles.seatsLabel}>{t('publish.seats')}</Text>
            <Stepper testID="publish-seats" value={seats} onChange={setSeats} min={1} max={6} />
          </View>
        </Card>

        {outcome ? <Hint>{outcome}</Hint> : null}

        <Button
          testID="publish-submit"
          label={t('publish.publish')}
          onPress={() => publish.mutate()}
          disabled={!from || !to || !weekdays.length || publish.isPending}
        />
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { gap: space.md, paddingBottom: 140 },
  label: { ...type.caption, color: colors.muted },
  days: { flexDirection: 'row', gap: 6 },
  times: { flexDirection: 'row', gap: space.sm },
  seats: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  seatsLabel: { ...type.body, color: colors.ink },
});

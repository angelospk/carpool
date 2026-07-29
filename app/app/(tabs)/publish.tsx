import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';

import { api } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { Button, Hint, Title, colors } from '../../src/components/ui';

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
  const [origin, setOrigin] = useState('');
  const [destination, setDestination] = useState('');
  const [weekdays, setWeekdays] = useState<number[]>([1, 2, 3, 4, 5]);
  const [earliest, setEarliest] = useState('07:30');
  const [latest, setLatest] = useState('08:00');
  const [seats, setSeats] = useState('3');
  const [outcome, setOutcome] = useState<string | null>(null);

  const parsePin = (s: string) => {
    const m = s.match(/^\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*(.*)$/);
    return m ? { lon: Number(m[1]), lat: Number(m[2]), label: m[3].trim() || s.trim() } : null;
  };

  const from = parsePin(origin);
  const to = parsePin(destination);

  const publish = useMutation({
    mutationFn: () =>
      api.publishRoute({
        origin_lon: from!.lon,
        origin_lat: from!.lat,
        destination_lon: to!.lon,
        destination_lat: to!.lat,
        origin_label: from!.label,
        destination_label: to!.label,
        seats: Number(seats) || 3,
        kind: 'recurring',
        weekdays,
        depart_earliest: earliest,
        depart_latest: latest,
      }),
    onSuccess: (r) => setOutcome(`${t('publish.published')} · ${r.route_km.toFixed(1)} km`),
    onError: (e) =>
      setOutcome(
        e instanceof ApiError && e.code === 'no_driver_profile'
          ? e.message
          : t('common.error'),
      ),
  });

  const toggle = (iso: number) =>
    setWeekdays((d) => (d.includes(iso) ? d.filter((x) => x !== iso) : [...d, iso].sort()));

  return (
    <ScrollView contentContainerStyle={styles.screen}>
      <Title>{t('publish.title')}</Title>

      <TextInput
        testID="publish-origin"
        style={styles.input}
        value={origin}
        onChangeText={setOrigin}
        placeholder="25.87, 40.85 Αλεξανδρούπολη"
        autoCapitalize="none"
      />
      <TextInput
        testID="publish-destination"
        style={styles.input}
        value={destination}
        onChangeText={setDestination}
        placeholder="26.17, 40.90 Φέρες"
        autoCapitalize="none"
      />

      <Text style={styles.label}>{t('publish.recurring')}</Text>
      <View style={styles.days}>
        {WEEKDAYS.map((d) => (
          <Pressable
            key={d.iso}
            testID={`weekday-${d.iso}`}
            onPress={() => toggle(d.iso)}
            style={[styles.day, weekdays.includes(d.iso) && styles.daySelected]}
          >
            <Text style={weekdays.includes(d.iso) ? styles.daySelectedLabel : undefined}>
              {d.label}
            </Text>
          </Pressable>
        ))}
      </View>

      <Text style={styles.label}>{t('publish.departWindow')}</Text>
      <View style={styles.times}>
        <TextInput
          testID="depart-earliest"
          style={[styles.input, styles.time]}
          value={earliest}
          onChangeText={setEarliest}
        />
        <TextInput
          testID="depart-latest"
          style={[styles.input, styles.time]}
          value={latest}
          onChangeText={setLatest}
        />
      </View>

      <Text style={styles.label}>{t('publish.seats')}</Text>
      <TextInput
        testID="publish-seats"
        style={styles.input}
        value={seats}
        onChangeText={(v) => setSeats(v.replace(/\D/g, '').slice(0, 1))}
        keyboardType="number-pad"
      />

      {outcome ? <Hint>{outcome}</Hint> : null}
      <Button
        testID="publish-submit"
        label={t('publish.publish')}
        onPress={() => publish.mutate()}
        disabled={!from || !to || !weekdays.length || publish.isPending}
      />
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  screen: { padding: 20, gap: 10, backgroundColor: colors.bg, flexGrow: 1 },
  label: { fontSize: 13, color: colors.muted, marginTop: 6 },
  input: { borderWidth: 1, borderColor: colors.border, borderRadius: 12, padding: 14, fontSize: 16 },
  days: { flexDirection: 'row', gap: 6 },
  day: {
    flex: 1,
    alignItems: 'center',
    paddingVertical: 12,
    borderRadius: 10,
    borderWidth: 1,
    borderColor: colors.border,
  },
  daySelected: { backgroundColor: colors.accent, borderColor: colors.accent },
  daySelectedLabel: { color: '#fff', fontWeight: '600' },
  times: { flexDirection: 'row', gap: 10 },
  time: { flex: 1, textAlign: 'center' },
});

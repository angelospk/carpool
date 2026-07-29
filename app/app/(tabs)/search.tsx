import { useState } from 'react';
import { StyleSheet, Text, TextInput, View } from 'react-native';
import { useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';

import { api } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { Button, Hint, Screen, Title, colors } from '../../src/components/ui';

// A pin is a coordinate plus the label the user recognises it by. The map picker
// fills both; typing coordinates directly is the fallback while the pilot runs
// without a geocoder.
type Pin = { lon: number; lat: number; label: string };

export default function SearchScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const [from, setFrom] = useState<Pin | null>(null);
  const [to, setTo] = useState<Pin | null>(null);
  const [seats, setSeats] = useState('1');
  const [outcome, setOutcome] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: () => {
      // A two-hour window from now is the common case; the picker refines it.
      const start = new Date();
      const end = new Date(start.getTime() + 2 * 60 * 60 * 1000);
      return api.createRequest({
        origin_lon: from!.lon,
        origin_lat: from!.lat,
        destination_lon: to!.lon,
        destination_lat: to!.lat,
        origin_label: from!.label,
        destination_label: to!.label,
        window_start: start.toISOString(),
        window_end: end.toISOString(),
        seats: Number(seats) || 1,
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
        e instanceof ApiError && e.code === 'outside_region'
          ? e.message
          : t('common.error'),
      ),
  });

  return (
    <Screen>
      <Title>{t('search.title')}</Title>
      <PinField testID="from" label={t('search.from')} pin={from} onChange={setFrom} />
      <PinField testID="to" label={t('search.to')} pin={to} onChange={setTo} />

      <Text style={styles.label}>{t('search.seats')}</Text>
      <TextInput
        testID="seats-input"
        style={styles.input}
        value={seats}
        onChangeText={(v) => setSeats(v.replace(/\D/g, '').slice(0, 1))}
        keyboardType="number-pad"
      />

      {outcome ? <Hint>{outcome}</Hint> : null}
      <Button
        testID="search-submit"
        label={t('search.post')}
        onPress={() => create.mutate()}
        disabled={!from || !to || create.isPending}
      />
    </Screen>
  );
}

function PinField({
  testID,
  label,
  pin,
  onChange,
}: {
  testID: string;
  label: string;
  pin: Pin | null;
  onChange: (p: Pin | null) => void;
}) {
  const [text, setText] = useState('');

  // "lon,lat Label" — deliberately blunt while there is no geocoder. The map
  // picker replaces this without changing the shape it produces.
  const parse = (value: string) => {
    setText(value);
    const m = value.match(/^\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*(.*)$/);
    if (!m) return onChange(null);
    onChange({ lon: Number(m[1]), lat: Number(m[2]), label: m[3].trim() || value.trim() });
  };

  return (
    <View style={styles.field}>
      <Text style={styles.label}>{label}</Text>
      <TextInput
        testID={`${testID}-input`}
        style={[styles.input, pin ? styles.inputValid : null]}
        value={text}
        onChangeText={parse}
        placeholder="25.87, 40.85 Αλεξανδρούπολη"
        autoCapitalize="none"
      />
    </View>
  );
}

const styles = StyleSheet.create({
  field: { gap: 4 },
  label: { fontSize: 13, color: colors.muted },
  input: { borderWidth: 1, borderColor: colors.border, borderRadius: 12, padding: 14, fontSize: 16 },
  inputValid: { borderColor: colors.accent },
});

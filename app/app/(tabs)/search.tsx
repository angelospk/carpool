import { useEffect, useRef, useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { api, type LuggageSize } from '../../src/api/endpoints';
import { refusalText } from '../../src/api/errorText';
import { Button, Card, Hint, Screen, Stepper, Title } from '../../src/components/ui';
import { PlaceField } from '../../src/components/PlaceField';
import { LuggagePicker } from '../../src/components/LuggagePicker';
import { WhenPicker } from '../../src/components/WhenPicker';
import { Estimate } from '../../src/components/Estimate';
import { LiveMapCard } from '../../src/components/LiveMapCard';
import { Supply } from '../../src/components/Supply';
import { colors, space, type } from '../../src/design/tokens';
import { pinsFromParams, type Pin } from '../../src/design/pin';
import { athensDate, departureWindow, nextDate, type Band, type Day } from '../../src/design/when';

export default function SearchScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const queryClient = useQueryClient();
  // The place itself rather than the text naming it: PlaceField only hands one
  // up once it refers to somewhere real, and takes it back the moment the text
  // is edited.
  const [from, setFrom] = useState<Pin | null>(null);
  const [to, setTo] = useState<Pin | null>(null);
  const [seats, setSeats] = useState(1);
  // "Now" keeps the behaviour this screen always had; the difference is that it
  // is now a visible answer rather than an assumption.
  const [day, setDay] = useState<Day>('today');
  const [band, setBand] = useState<Band>('now');
  const [luggage, setLuggage] = useState<LuggageSize>('none');
  const [luggageCount, setLuggageCount] = useState(0);
  const [outcome, setOutcome] = useState<string | null>(null);

  // Arriving from a suggestion on the home screen, both ends already known.
  //
  // The tab stays mounted, so this cannot simply run on every render: it would
  // overwrite whatever the person typed after they got here. `preset` is the
  // identity of the link, so pressing the same suggestion again fills the form
  // again, while editing it afterwards is left alone.
  const params = useLocalSearchParams<Record<string, string | string[]>>();
  const preset = Array.isArray(params.preset) ? params.preset[0] : params.preset;
  const applied = useRef<string | null>(null);
  useEffect(() => {
    if (!preset || applied.current === preset) return;
    const pins = pinsFromParams(params);
    if (!pins) return;

    applied.current = preset;
    setFrom(pins.from);
    setTo(pins.to);
  }, [preset, params]);

  const create = useMutation({
    mutationFn: () => {
      // Read at the moment of sending rather than at render: somebody can sit
      // on this screen for ten minutes, and "now" has to mean now.
      const { start, end } = departureWindow(day, band, new Date());
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
        luggage,
        luggage_count: luggageCount,
      });
    },
    onSuccess: (res) => {
      // No driver yet is a normal outcome, not an error: the request stays open
      // and the server asks someone when a route appears.
      setOutcome(res.asking ? t('search.asking') : t('search.noDrivers'));
      // It belongs on the home screen from this moment, not from whenever that
      // screen next happens to refetch.
      void queryClient.invalidateQueries({ queryKey: ['requests'] });
      router.push(`/request/${res.request_id}`);
    },
    onError: (e) =>
      setOutcome(refusalText(e) ?? t('common.error')),
  });

  return (
    <Screen topInset>
      <ScrollView contentContainerStyle={styles.body} showsVerticalScrollIndicator={false}>
        <Title>{t('search.title')}</Title>
        {/* Who is already on the road, before asking: a seat is likelier
            on a road somebody is driving. */}
        <LiveMapCard />

        <Card>
          <View style={styles.pins}>
            <View style={styles.rail}>
              <View style={[styles.dot, from && styles.dotSet]} />
              <View style={styles.line} />
              <View style={[styles.dot, to && styles.dotSet]} />
            </View>
            <View style={styles.fields}>
              <PlaceField
                testID="from"
                label={t('search.from')}
                value={from}
                onChange={setFrom}
                placeholder={t('search.placeholder')}
                here
              />
              <PlaceField
                testID="to"
                label={t('search.to')}
                value={to}
                onChange={setTo}
                placeholder={t('search.placeholder')}
              />
            </View>
          </View>

          <Estimate testID="estimate" from={from} to={to} />

          <WhenPicker
            testID="when"
            day={day}
            band={band}
            onChange={(next) => {
              setDay(next.day);
              setBand(next.band);
            }}
          />

          <Supply
            from={from}
            to={to}
            date={day === 'today' ? athensDate(new Date()) : nextDate(athensDate(new Date()))}
          />

          <LuggagePicker
            testID="luggage"
            value={luggage}
            onChange={setLuggage}
            count={luggageCount}
            onCountChange={setLuggageCount}
          />

          <View style={styles.seats}>
            <Text style={styles.seatsLabel}>{t('search.seats')}</Text>
            <Stepper testID="seats" value={seats} onChange={setSeats} min={1} max={4} />
          </View>
        </Card>

        {outcome ? <Hint>{outcome}</Hint> : null}

        <Hint>{t('search.whatHappens')}</Hint>

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

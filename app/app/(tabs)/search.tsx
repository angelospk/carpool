import { useEffect, useRef, useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { api, type LuggageSize, type MeetingPoint } from '../../src/api/endpoints';
import { refusalText } from '../../src/api/errorText';
import { ApiError } from '../../src/api/client';
import { BottomBar, Button, Card, Hint, Screen, Stepper, Title } from '../../src/components/ui';
import { PlaceField } from '../../src/components/PlaceField';
import { MeetingPointField } from '../../src/components/MeetingPointField';
import { LuggagePicker } from '../../src/components/LuggagePicker';
import { WhenPicker } from '../../src/components/WhenPicker';
import { Estimate } from '../../src/components/Estimate';
import { LiveMapCard } from '../../src/components/LiveMapCard';
import { Supply } from '../../src/components/Supply';
import { colors, space, type } from '../../src/design/tokens';
import { usePushReach } from '../../src/push/reach';
import { pinsFromParams, pointLabel, type Pin } from '../../src/design/pin';
import { dateOf, departureWindow, type Band, type Day } from '../../src/design/when';

export default function SearchScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const queryClient = useQueryClient();
  const on = usePushReach((s) => s.on);
  // The place itself rather than the text naming it: PlaceField only hands one
  // up once it refers to somewhere real, and takes it back the moment the text
  // is edited.
  const [from, setFrom] = useState<Pin | null>(null);
  const [to, setTo] = useState<Pin | null>(null);
  // Where exactly, as a driver says it: the middle of the village is not
  // always where a car can stop. Φέρες sits 4 km off the road that passes it,
  // and never matched a driver on it (QA 2026-10-16, U4).
  const [fromPoint, setFromPoint] = useState<MeetingPoint | null>(null);
  const [toPoint, setToPoint] = useState<MeetingPoint | null>(null);
  const [seats, setSeats] = useState(1);
  // "Now" keeps the behaviour this screen always had; the difference is that it
  // is now a visible answer rather than an assumption.
  const [day, setDay] = useState<Day>('today');
  const [band, setBand] = useState<Band>('now');
  // Other days that would do as well. Each is its own request, pointed at the
  // first, so the first one a driver takes withdraws the rest.
  const [extra, setExtra] = useState<string[]>([]);
  // Days that failed to save, with the request they are other days of. The
  // next press sends only these: sending the first day again would make a new
  // request if a driver had taken it meanwhile, and carry the passenger twice.
  const [unsent, setUnsent] = useState<{ root: string; days: string[] } | null>(null);
  const [luggage, setLuggage] = useState<LuggageSize>('none');
  const [luggageCount, setLuggageCount] = useState(0);
  const [outcome, setOutcome] = useState<string | null>(null);

  // Arriving from a suggestion on the home screen, both ends already known.
  //
  // The tab stays mounted, so this cannot simply run on every render: it would
  // overwrite whatever the person typed after they got here. `preset` is the
  // identity of the link, so pressing the same suggestion again fills the form
  // again, while editing it afterwards is left alone.
  // A changed form is a new request, not the rest of the old one.
  useEffect(() => {
    setUnsent(null);
  }, [from, to, fromPoint, toPoint, day, band, extra, seats, luggage, luggageCount]);

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
    setFromPoint(null);
    setToPoint(null);
  }, [preset, params]);

  const create = useMutation({
    mutationFn: async () => {
      // Read at the moment of sending rather than at render: somebody can sit
      // on this screen for ten minutes, and "now" has to mean now.
      const now = new Date();
      const post = (d: Day, b: Band, alternativeOf?: string) => {
        const { start, end } = departureWindow(d, b, now);
        return api.createRequest({
          origin_lon: fromPoint?.lon ?? (from as Pin).lon,
          origin_lat: fromPoint?.lat ?? (from as Pin).lat,
          destination_lon: toPoint?.lon ?? (to as Pin).lon,
          destination_lat: toPoint?.lat ?? (to as Pin).lat,
          origin_label: pointLabel(from as Pin, fromPoint),
          destination_label: pointLabel(to as Pin, toPoint),
          window_start: start.toISOString(),
          window_end: end.toISOString(),
          seats,
          luggage,
          luggage_count: luggageCount,
          ...(alternativeOf ? { alternative_of: alternativeOf } : {}),
        });
      };
      const first = unsent
        ? { request_id: unsent.root, asking: false }
        : await post(day, band);
      // One at a time, in date order. A day that fails does not stop the rest.
      // A refusal is final, unlike a dropped connection: the journey was taken
      // or cancelled meanwhile, and asking again gets the same answer.
      const failed: string[] = [];
      for (const d of unsent ? unsent.days : extra) {
        if (!unsent && d === dateOf(day, now)) continue;
        try {
          await post(d as Day, band === 'now' ? 'morning' : band, first.request_id);
        } catch (e) {
          if (e instanceof ApiError && e.status >= 400 && e.status < 500) {
            setUnsent(null);
            void queryClient.invalidateQueries({ queryKey: ['requests'] });
            throw e;
          }
          failed.push(d);
        }
      }
      return { ...first, failed };
    },
    onSuccess: (res) => {
      if (res.failed.length > 0) {
        setUnsent({ root: res.request_id, days: res.failed });
        void queryClient.invalidateQueries({ queryKey: ['requests'] });
        setOutcome(t('search.daysFailed', { count: res.failed.length, post: t('search.post') }));
        return;
      }
      setUnsent(null);
      // No driver yet is a normal outcome, not an error: the request stays open
      // and the server asks someone when a route appears.
      setOutcome(res.asking ? t('search.asking') : t(on ? 'search.noDrivers' : 'search.noDriversNoPush'));
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
                onChange={(p) => {
                  setFrom(p);
                  setFromPoint(null);
                }}
                placeholder={t('search.placeholder')}
                near={to}
                here
              />
              <MeetingPointField testID="from-point" place={from} value={fromPoint} onChange={setFromPoint} />
              <PlaceField
                testID="to"
                label={t('search.to')}
                value={to}
                onChange={(p) => {
                  setTo(p);
                  setToPoint(null);
                }}
                placeholder={t('search.placeholder')}
                near={from}
              />
              <MeetingPointField testID="to-point" place={to} value={toPoint} onChange={setToPoint} />
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
            extra={extra}
            onExtraChange={setExtra}
          />

          <Supply
            from={from}
            to={to}
            date={dateOf(day, new Date())}
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

        <Hint>{t(on ? 'search.whatHappens' : 'search.whatHappensNoPush')}</Hint>
      </ScrollView>
      <BottomBar bleed inset={false}>
        {outcome ? <Hint>{outcome}</Hint> : null}
        <Button
          testID="search-submit"
          label={t('search.post')}
          dot
          onPress={() => create.mutate()}
          disabled={!from || !to || create.isPending}
        />
      </BottomBar>
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { gap: space.lg, paddingBottom: space.xl },
  pins: { flexDirection: 'row', gap: space.md },
  rail: { alignItems: 'center', paddingVertical: 26 },
  dot: { width: 12, height: 12, borderRadius: 6, borderWidth: 2.5, borderColor: colors.line },
  dotSet: { borderColor: colors.ink, backgroundColor: colors.ink },
  line: { flex: 1, width: 2, backgroundColor: colors.line, minHeight: 20 },
  fields: { flex: 1, gap: space.sm },
  seats: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  seatsLabel: { ...type.body, color: colors.ink },
});

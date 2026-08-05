import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';

import { api, type LuggageSize, type MeetingPoint } from '../../src/api/endpoints';
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
import { VehiclePicker } from '../../src/components/VehiclePicker';
import { LuggagePicker } from '../../src/components/LuggagePicker';
import { ContributionChoice } from '../../src/components/ContributionChoice';
import { colors, space, type } from '../../src/design/tokens';
import { type Pin } from '../../src/design/pin';
import { PlaceField } from '../../src/components/PlaceField';
import { MeetingPointField } from '../../src/components/MeetingPointField';

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

  // Where exactly, once the town is settled. Kept next to the place it belongs
  // to rather than in a section of its own: "Alexandroupoli, at the KTEL" is
  // one answer, not two.
  const [fromPoint, setFromPoint] = useState<MeetingPoint | null>(null);
  const [toPoint, setToPoint] = useState<MeetingPoint | null>(null);

  // null means "whatever I set on my profile". The form only sends a detour
  // once it has been touched, so a driver who never opens this keeps their own
  // default rather than having the screen's idea of one imposed on them.
  const [allowStops, setAllowStops] = useState<boolean | null>(null);
  const [detourKm, setDetourKm] = useState<number | null>(null);
  // null means "whichever is my default", which is what a driver with one car
  // sends and what the server would have chosen anyway.
  const [vehicleID, setVehicleID] = useState<string | null>(null);
  const [luggage, setLuggage] = useState<LuggageSize>('suitcase');
  // All of it until the driver says otherwise, which is what every route
  // published before they could choose was implicitly asking for.
  const [share, setShare] = useState(1);
  const [outcome, setOutcome] = useState<string | null>(null);


  const publish = useMutation({
    mutationFn: () =>
      api.publishRoute({
        // The landmark when one was chosen, the settlement otherwise. This is
        // the whole point of offering them: "Φέρες" is a town and "Φέρες, στο
        // βενζινάδικο" is somewhere a car can stop.
        origin_lon: fromPoint?.lon ?? (from as Pin).lon,
        origin_lat: fromPoint?.lat ?? (from as Pin).lat,
        destination_lon: toPoint?.lon ?? (to as Pin).lon,
        destination_lat: toPoint?.lat ?? (to as Pin).lat,
        origin_label: label(from as Pin, fromPoint),
        destination_label: label(to as Pin, toPoint),
        seats,
        kind: 'recurring',
        weekdays,
        depart_earliest: earliest,
        depart_latest: latest,
        ...(allowStops !== null ? { allow_stops: allowStops } : {}),
        ...(detourKm !== null ? { detour_km: detourKm } : {}),
        ...(vehicleID !== null ? { vehicle_id: vehicleID } : {}),
        luggage,
        contribution_share: share,
      }),
    onSuccess: (r) => setOutcome(`${t('publish.published')} · ${r.route_km.toFixed(1)} km`),
    onError: (e) =>
      setOutcome(
        e instanceof ApiError && (e.code === 'no_driver_profile' || e.code === 'no_vehicle')
          ? e.message
          : t('common.error'),
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
            onChange={(p) => {
              setFrom(p);
              setFromPoint(null);
            }}
            placeholder={t('search.placeholder')}
          />
          <MeetingPointField
            testID="publish-origin-point"
            place={from}
            value={fromPoint}
            onChange={setFromPoint}
          />
          <PlaceField
            testID="publish-destination"
            label={t('search.to')}
            value={to}
            onChange={(p) => {
              setTo(p);
              setToPoint(null);
            }}
            placeholder={t('search.placeholder')}
          />
          <MeetingPointField
            testID="publish-destination-point"
            place={to}
            value={toPoint}
            onChange={setToPoint}
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

          <VehiclePicker testID="publish-vehicle" value={vehicleID} onChange={setVehicleID} />

          <LuggagePicker testID="publish-luggage" value={luggage} onChange={setLuggage} asDriver />

          <ContributionChoice testID="publish-share" value={share} onChange={setShare} />

          <View style={styles.seats}>
            <Text style={styles.seatsLabel}>{t('publish.seats')}</Text>
            <Stepper testID="publish-seats" value={seats} onChange={setSeats} min={1} max={6} />
          </View>
        </Card>

        {/* How far out of the way, for this trip. It is a question about today:
            the long run to Thessaloniki has room in it and the Monday commute
            has none, which is why the answer is not kept on the profile.

            Untouched, it says so rather than showing a figure. The screen does
            not know what this driver's profile says, and inventing a plausible
            "10 km, stops on" would be telling them something about their own
            settings that may not be true. */}
        <SectionHeader title={t('publish.detour')} />
        <Card>
          {allowStops === null && detourKm === null ? (
            <Pressable testID="detour-override" onPress={() => setAllowStops(true)}>
              <Text style={styles.seatsLabel}>{t('publish.detourFromProfile')}</Text>
              <Text style={styles.hint}>{t('publish.detourChange')}</Text>
            </Pressable>
          ) : (
            <>
              <Pressable
                testID="detour-allow"
                onPress={() => setAllowStops((a) => a === false)}
                style={styles.seats}
              >
                <Text style={styles.seatsLabel}>{t('publish.allowStops')}</Text>
                <Text style={styles.toggle}>
                  {allowStops === false ? t('common.off') : t('common.on')}
                </Text>
              </Pressable>

              {allowStops === false ? (
                <Text style={styles.hint}>{t('publish.noStopsHint')}</Text>
              ) : (
                <>
                  <View style={styles.divider} />
                  <View style={styles.seats}>
                    <Text style={styles.seatsLabel}>{t('publish.detourKm')}</Text>
                    <Stepper
                      testID="detour-km"
                      value={detourKm ?? defaultDetourKm}
                      // Choosing a distance is agreeing to stops. Sending the
                      // distance alone would leave a driver whose profile says
                      // no stops setting a detour that can never be taken.
                      onChange={(v) => {
                        setDetourKm(v);
                        setAllowStops(true);
                      }}
                      min={0}
                      max={50}
                      step={5}
                    />
                  </View>
                  <Text style={styles.hint}>{t('publish.detourHint')}</Text>
                </>
              )}
            </>
          )}
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
  toggle: { ...type.label, color: colors.primary },
  hint: { ...type.caption, color: colors.muted, marginTop: space.xs },
  divider: { height: 1, backgroundColor: colors.line, marginVertical: space.sm },
});

// Where the stepper starts once a driver has said they want to set a figure for
// this trip. Until then nothing is shown and nothing is sent, so this is never
// mistaken for what their profile holds.
const defaultDetourKm = 10;

// "Φέρες, ΚΤΕΛ Έβρου" rather than either alone: the town is what somebody
// recognises in a list and the landmark is what they drive to.
function label(pin: Pin, point: MeetingPoint | null): string {
  return point ? `${pin.label}, ${point.name}` : pin.label;
}

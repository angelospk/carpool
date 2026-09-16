import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery } from '@tanstack/react-query';

import {
  api,
  type LuggageSize,
  type MeetingPoint,
  type PricingMode,
} from '../../src/api/endpoints';
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
import { BootShare } from '../../src/components/BootShare';
import { ContributionChoice } from '../../src/components/ContributionChoice';
import { colors, space, type } from '../../src/design/tokens';
import { type Pin } from '../../src/design/pin';
import { PlaceField } from '../../src/components/PlaceField';
import { MeetingPointField } from '../../src/components/MeetingPointField';
import { ShareRouteButton } from '../../src/components/ShareRouteButton';
import { Demand } from '../../src/components/Demand';
import type { ShareableRoute } from '../../src/design/shareRoute';

// The three answers that cover almost every published route. They replace the
// set rather than adding to it: "καθημερινές" is a whole schedule, not five
// separate taps, and a driver who wants Monday and Thursday still has the days
// themselves underneath.
const PRESETS = [
  { key: 'presetWeekdays', days: [1, 2, 3, 4, 5] },
  { key: 'presetWeekend', days: [6, 7] },
  { key: 'presetEveryday', days: [1, 2, 3, 4, 5, 6, 7] },
] as const;

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
  const [luggageCapacity, setLuggageCapacity] = useState(3);
  // All of it until the driver says otherwise, which is what every route
  // published before they could choose was implicitly asking for. The fraction
  // is kept while the open contribution is selected, so switching to it and
  // back does not quietly reset a driver who had chosen half.
  const [share, setShare] = useState(1);
  const [pricing, setPricing] = useState<PricingMode>('formula');

  // Shares react-query's cache with the picker below, so asking here costs no
  // request. It is asked because an empty garage is the reason publishing is
  // about to fail, and the button should say so before it is pressed.
  const garage = useQuery({ queryKey: ['vehicles'], queryFn: api.vehicles });
  const noCar = garage.isSuccess && garage.data.length === 0;
  const [outcome, setOutcome] = useState<string | null>(null);
  const [published, setPublished] = useState<ShareableRoute | null>(null);


  const publish = useMutation({
    mutationFn: (snap: ShareableRoute) =>
      api.publishRoute({
        // The landmark when one was chosen, the settlement otherwise. This is
        // the whole point of offering them: "Φέρες" is a town and "Φέρες, στο
        // βενζινάδικο" is somewhere a car can stop.
        origin_lon: fromPoint?.lon ?? (from as Pin).lon,
        origin_lat: fromPoint?.lat ?? (from as Pin).lat,
        destination_lon: toPoint?.lon ?? (to as Pin).lon,
        destination_lat: toPoint?.lat ?? (to as Pin).lat,
        origin_label: snap.origin_label,
        destination_label: snap.destination_label,
        seats: snap.seats,
        kind: 'recurring',
        weekdays: snap.weekdays,
        depart_earliest: snap.depart_earliest,
        depart_latest: latest,
        ...(allowStops !== null ? { allow_stops: allowStops } : {}),
        ...(detourKm !== null ? { detour_km: detourKm } : {}),
        ...(vehicleID !== null ? { vehicle_id: vehicleID } : {}),
        luggage,
        luggage_capacity: luggageCapacity,
        pricing_mode: pricing,
        // A fraction only means something under the formula. The server
        // normalises it either way; sending it anyway would put a number in
        // the request that the receipt could not later explain.
        ...(pricing === 'formula' ? { contribution_share: share } : {}),
      }),
    onSuccess: (r, snap) => {
      setOutcome(`${t('publish.published')} · ${r.route_km.toFixed(1)} km`);
      setPublished(snap);
    },
    onError: (e) => {
      setOutcome(
        // The server's own wording is English, and it reached the screen
        // verbatim: a Greek pilot user was told "add your car details before
        // publishing a route". The code is the contract; the sentence is ours.
        e instanceof ApiError && (e.code === 'no_driver_profile' || e.code === 'no_vehicle')
          ? t('publish.noDriverProfile')
          : t('common.error'),
      );
      // A publish that failed leaves nothing published, so there is nothing
      // to share.
      setPublished(null);
    },
  });

  const toggle = (iso: number) =>
    setWeekdays((d) => (d.includes(iso) ? d.filter((x) => x !== iso) : [...d, iso].sort()));

  // The boot calculator can land on zero, and zero cannot be sent alongside a
  // luggage size: `route_templates` pairs a capacity of zero with 'none'.
  const noBootLeft = luggage !== 'none' && luggageCapacity < 1;

  return (
    <Screen topInset>
      <ScrollView contentContainerStyle={styles.body} showsVerticalScrollIndicator={false}>
        <Title>{t('publish.title')}</Title>

        {/* Whoever is already looking for a lift, before the form asks for
            anything. A driver deciding whether this is worth ninety seconds is
            deciding it here. */}
        <Demand />

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

        {/* The schedule is when the car goes, and nothing else. Seats, bags and
            the fuel used to live in this card too, which made "πρόγραμμα" the
            name of a box holding everything the form had not found a home
            for. */}
        <SectionHeader title={t('publish.schedule')} />
        <Card>
          <Text style={styles.label}>{t('publish.recurring')}</Text>
          <View style={styles.days}>
            {PRESETS.map((p) => (
              <Chip
                key={p.key}
                testID={`preset-${p.key}`}
                label={t(`publish.${p.key}`)}
                selected={sameDays(weekdays, p.days)}
                onPress={() => setWeekdays([...p.days])}
              />
            ))}
          </View>
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
        </Card>

        <SectionHeader title={t('publish.carSection')} />
        <Card>
          <VehiclePicker testID="publish-vehicle" value={vehicleID} onChange={setVehicleID} />
        </Card>

        <SectionHeader title={t('publish.seatsSection')} />
        <Card>
          <View style={styles.seats}>
            <Text style={styles.seatsLabel}>{t('publish.seats')}</Text>
            <Stepper testID="publish-seats" value={seats} onChange={setSeats} min={1} max={6} />
          </View>

          <View style={styles.divider} />

          <LuggagePicker
            testID="publish-luggage"
            value={luggage}
            onChange={setLuggage}
            count={luggageCapacity}
            onCountChange={setLuggageCapacity}
            asDriver
          />

          {/* Only while a size is chosen: with 'none' there is no capacity to
              divide up, and the picker above has already hidden its own count
              for the same reason. */}
          {luggage !== 'none' ? (
            <BootShare testID="publish-boot" onResult={setLuggageCapacity} />
          ) : null}
        </Card>

        <SectionHeader title={t('contribution.title')} />
        <Card>
          <ContributionChoice
            testID="publish-share"
            mode={pricing}
            onModeChange={setPricing}
            share={share}
            onShareChange={setShare}
          />
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
        {published !== null ? (
          <ShareRouteButton
            route={published}
            testID="publish-share-route"
            failedTestID="publish-share-failed"
          />
        ) : null}
        {noCar ? <Hint>{t('publish.noDriverProfile')}</Hint> : null}
        {/* A boot with nothing left in it and a luggage size still chosen is a
            pair the database refuses. Saying so here is better than a 400 after
            the driver has filled in everything else. */}
        {noBootLeft ? <Hint>{t('publish.bootFullHint')}</Hint> : null}

        <Button
          testID="publish-submit"
          label={t('publish.publish')}
          // The snapshot is what was sent, not what the form says later: the
          // form stays editable after publishing, and sharing the edited
          // fields would send a schedule nobody can be picked up on.
          onPress={() =>
            publish.mutate({
              origin_label: label(from as Pin, fromPoint),
              destination_label: label(to as Pin, toPoint),
              kind: 'recurring',
              weekdays: [...weekdays],
              depart_earliest: earliest,
              seats,
            })
          }
          disabled={!from || !to || !weekdays.length || noCar || noBootLeft || publish.isPending}
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

// Whether the days chosen are exactly a preset's, so the preset can read as
// selected. Order is not part of the answer: the day chips keep the list
// sorted, but a preset is a set either way.
function sameDays(chosen: number[], preset: readonly number[]): boolean {
  return chosen.length === preset.length && preset.every((d) => chosen.includes(d));
}

// "Φέρες, ΚΤΕΛ Έβρου" rather than either alone: the town is what somebody
// recognises in a list and the landmark is what they drive to.
function label(pin: Pin, point: MeetingPoint | null): string {
  return point ? `${pin.label}, ${point.name}` : pin.label;
}

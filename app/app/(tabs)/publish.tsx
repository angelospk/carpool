import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useFocusEffect, useLocalSearchParams, useRouter } from 'expo-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import {
  api,
  LUGGAGE_SIZES,
  type LuggageSize,
  type MeetingPoint,
  type PricingMode,
} from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { refusalText } from '../../src/api/errorText';
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
import { pinsFromParams, type Pin } from '../../src/design/pin';
import { PlaceField } from '../../src/components/PlaceField';
import { MeetingPointField } from '../../src/components/MeetingPointField';
import { ShareRouteButton } from '../../src/components/ShareRouteButton';
import { Demand } from '../../src/components/Demand';
import type { ShareableRoute } from '../../src/design/shareRoute';
import { athensDate, nextDate } from '../../src/design/when';
import { athensTime, km } from '../../src/design/format';
import { calendarDay } from '../../src/design/format';

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
  // A trip that happens once — Saturday to Θεσσαλονίκη — rather than a commute.
  // The date defaults to tomorrow, the likeliest answer when somebody opens
  // this to offer a single journey.
  const [kind, setKind] = useState<'recurring' | 'oneoff'>('recurring');
  // The tab stays mounted, possibly overnight, so "today" is read again every
  // time the screen comes back into view rather than once when it was built.
  const [today, setToday] = useState(() => athensDate(new Date()));
  useFocusEffect(
    useCallback(() => {
      setToday(athensDate(new Date()));
    }, []),
  );
  const dates = useMemo(() => nextDates(today, ONEOFF_DAYS), [today]);
  const [oneoffDate, setOneoffDate] = useState(dates[1]);
  // A choice that is still on offer stays; one that the day has passed moves to
  // the first day there is, rather than going out and being refused as past.
  useEffect(() => {
    if (!dates.includes(oneoffDate)) setOneoffDate(dates[0]);
  }, [dates, oneoffDate]);
  const [earliest, setEarliest] = useState('07:30');
  const [latest, setLatest] = useState('08:00');
  // «Φεύγω τώρα»: a trip for the next quarter of an hour, published and
  // opened at once, where «Φεύγω» is. A one-off for today whose window is
  // worked out when the button is pressed, so it never goes out in the past.
  const [leavingNow, setLeavingNow] = useState(false);
  const nowWindow = useRef<{ latest: string } | null>(null);
  const router = useRouter();
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

  // Who the route is offered to. Null is everybody, and it is the only choice
  // for somebody in no group, so the section is not shown to them at all.
  const myGroups = useQuery({ queryKey: ['groups'], queryFn: () => api.myGroups() });
  const [groupID, setGroupID] = useState<string | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);
  const [published, setPublished] = useState<ShareableRoute | null>(null);

  // «Αλλαγή» from the list of routes: the form arrives filled in with the
  // route, and publishing puts out the changed one and retires the old, which
  // calls off its empty trips. Applied once per route, because the tab stays
  // mounted and re-applying would undo what the driver then edited.
  const params = useLocalSearchParams<Record<string, string | string[]>>();
  const [replaces, setReplaces] = useState<{ id: string; label: string } | null>(null);
  const applied = useRef<string | null>(null);
  const queryClient = useQueryClient();
  useEffect(() => {
    const id = first(params.replaces);
    if (!id || applied.current === id) return;
    const pins = pinsFromParams(params);
    if (!pins) return;
    applied.current = id;
    setFrom(pins.from);
    setTo(pins.to);
    setFromPoint(null);
    setToPoint(null);
    setLeavingNow(false);
    const oneoff = first(params.oneoff_date);
    if (first(params.kind) === 'oneoff' && oneoff) {
      setKind('oneoff');
      setOneoffDate(oneoff);
    } else {
      setKind('recurring');
      const days = (first(params.weekdays) ?? '').split(',').map(Number).filter((d) => d >= 1 && d <= 7);
      if (days.length) setWeekdays(days);
    }
    const earliestParam = first(params.earliest);
    const latestParam = first(params.latest);
    if (earliestParam) setEarliest(earliestParam);
    if (latestParam) setLatest(latestParam);
    const seatsParam = Number(first(params.seats));
    if (seatsParam >= 1) setSeats(seatsParam);
    // Who it is offered to above all: a group-only route republished without
    // its group would go out to everybody.
    setGroupID(first(params.group) ?? null);
    setVehicleID(first(params.vehicle) ?? null);
    const pricingParam = first(params.pricing);
    if (pricingParam === 'formula' || pricingParam === 'free_contribution' || pricingParam === 'free') {
      setPricing(pricingParam);
    }
    const shareParam = Number(first(params.share));
    if (shareParam > 0 && shareParam <= 1) setShare(shareParam);
    const luggageParam = first(params.luggage);
    if (luggageParam && (LUGGAGE_SIZES as readonly string[]).includes(luggageParam)) {
      setLuggage(luggageParam as LuggageSize);
    }
    const capacityParam = Number(first(params.luggage_capacity));
    if (first(params.luggage_capacity) !== undefined && Number.isInteger(capacityParam) && capacityParam >= 0) {
      setLuggageCapacity(capacityParam);
    }
    const stopsParam = first(params.allow_stops);
    if (stopsParam === 'true' || stopsParam === 'false') setAllowStops(stopsParam === 'true');
    const detourParam = Number(first(params.detour_km));
    if (first(params.detour_km) !== undefined && detourParam >= 0) setDetourKm(detourParam);
    setReplaces({ id, label: `${pins.from.label} → ${pins.to.label}` });
  }, [params]);


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
        kind: snap.kind === 'oneoff' ? 'oneoff' : 'recurring',
        ...(snap.kind === 'oneoff'
          ? { oneoff_date: snap.oneoff_date }
          : { weekdays: snap.weekdays }),
        depart_earliest: snap.depart_earliest,
        depart_latest: nowWindow.current?.latest ?? latest,
        ...(allowStops !== null ? { allow_stops: allowStops } : {}),
        ...(detourKm !== null ? { detour_km: detourKm } : {}),
        ...(vehicleID !== null ? { vehicle_id: vehicleID } : {}),
        ...(groupID !== null ? { group_id: groupID } : {}),
        luggage,
        luggage_capacity: luggageCapacity,
        pricing_mode: pricing,
        // A fraction only means something under the formula. The server
        // normalises it either way; sending it anyway would put a number in
        // the request that the receipt could not later explain.
        ...(pricing === 'formula' ? { contribution_share: share } : {}),
      }),
    // Cleared when an attempt starts, not when one fails. The route published a
    // minute ago still exists after a second attempt goes wrong, so "a publish
    // that failed leaves nothing published" was simply untrue from the second
    // press onwards. What this button means is "the route you just published",
    // and the moment that stops being the earlier one is the moment a new
    // attempt begins — including while it is still in flight.
    onMutate: () => {
      // The outcome goes with it. Otherwise a second attempt runs underneath
      // "Η διαδρομή δημοσιεύτηκε · 62,4 km" from the first one, which reads as
      // a result for the request still in flight.
      setOutcome(null);
      setPublished(null);
    },
    onSuccess: (r, snap) => {
      setOutcome(`${t('publish.published')} · ${km(r.route_km)}`);
      setPublished({ ...snap, id: r.template_id });
      // The places go, so a second press cannot publish the same route again
      // and the next route starts from empty. Schedule, seats and car stay: a
      // driver's next route usually shares them.
      setFrom(null);
      setTo(null);
      setFromPoint(null);
      setToPoint(null);
      setLeavingNow(false);
      if (replaces) {
        // After the new one exists, never before: a failed publish must not
        // leave the driver with neither.
        const old = replaces.id;
        setReplaces(null);
        void api
          .retireRoute(old)
          .then(() => queryClient.invalidateQueries({ queryKey: ['my-routes'] }))
          .catch(() => setOutcome(t('publish.replaceFailed')));
      }
      if (nowWindow.current && r.first_occurrence_id) {
        router.push({ pathname: '/trip/[id]', params: { id: r.first_occurrence_id } });
      }
    },
    onError: (e) => {
      const refused = refusalText(e);
      if (refused) return setOutcome(refused);
      setOutcome(
        // The server's own wording is English, and it reached the screen
        // verbatim: a Greek pilot user was told "add your car details before
        // publishing a route". The code is the contract; the sentence is ours.
        e instanceof ApiError && (e.code === 'no_driver_profile' || e.code === 'no_vehicle')
          ? t('publish.noDriverProfile')
          : // A place the routing extract does not reach. Its own sentence,
            // because "something went wrong" sends the driver back to press the
            // same button on the same two pins.
            e instanceof ApiError && e.code === 'outside_service_area'
            ? t('publish.outsideServiceArea')
            : t('common.error'),
      );
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
        {replaces ? (
          <Hint testID="editing-route">{t('publish.replacing', { route: replaces.label })}</Hint>
        ) : null}

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
            here
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
          <View style={styles.days}>
            <Chip
              testID="schedule-recurring"
              label={t('publish.everyWeek')}
              selected={kind === 'recurring'}
              onPress={() => {
                setLeavingNow(false);
                setKind('recurring');
              }}
            />
            <Chip
              testID="schedule-oneoff"
              label={t('publish.once')}
              selected={kind === 'oneoff' && !leavingNow}
              onPress={() => {
                setLeavingNow(false);
                setKind('oneoff');
              }}
            />
            <Chip
              testID="schedule-now"
              label={t('publish.leavingNow')}
              selected={leavingNow}
              onPress={() => {
                // A trip now is not a new version of the route being changed,
                // so publishing it must not retire that route.
                setReplaces(null);
                setLeavingNow(true);
                setKind('oneoff');
              }}
            />
          </View>

          {leavingNow ? (
            <Hint testID="leaving-now-hint">{t('publish.leavingNowHint')}</Hint>
          ) : kind === 'oneoff' ? (
            <>
              <Text style={styles.label}>{t('publish.onDate')}</Text>
              <View style={[styles.days, styles.wrap]}>
                {dates.map((d, i) => (
                  <Chip
                    fit
                    key={d}
                    testID={`oneoff-date-${i}`}
                    label={
                      i === 0 ? t('publish.today') : i === 1 ? t('publish.tomorrow') : calendarDay(d)
                    }
                    selected={oneoffDate === d}
                    onPress={() => setOneoffDate(d)}
                  />
                ))}
              </View>
            </>
          ) : (
            <>
            <Text style={styles.label}>{t('publish.recurring')}</Text>
            <View style={[styles.days, styles.wrap]}>
              {PRESETS.map((p) => (
                <Chip
                  fit
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
            </>
          )}

          {leavingNow ? null : (
          <>
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
          </>
          )}
        </Card>

        {myGroups.isError ? (
          <>
            <Hint testID="audience-failed">{t('common.error')}</Hint>
            <Button
              testID="audience-retry"
              label={t('common.retry')}
              variant="secondary"
              onPress={() => void myGroups.refetch()}
            />
          </>
        ) : null}
        {myGroups.data && myGroups.data.groups.length > 0 ? (
          <>
            <SectionHeader title={t('groups.audience')} />
            <Card>
              <View style={[styles.days, styles.wrap]}>
                <Chip
                  testID="audience-everybody"
                  label={t('groups.everybody')}
                  selected={groupID === null}
                  onPress={() => setGroupID(null)}
                />
                {myGroups.data.groups.map((g) => (
                  <Chip
                    key={g.id}
                    testID={`audience-${g.id}`}
                    label={t('groups.onlyGroup', { name: g.name })}
                    selected={groupID === g.id}
                    onPress={() => setGroupID(g.id)}
                  />
                ))}
              </View>
            </Card>
          </>
        ) : null}

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
          onPress={() => {
            // Leaving now: from two minutes ahead, which the server accepts as
            // not past, to a quarter of an hour after that, within today.
            const at = Date.now();
            nowWindow.current = leavingNow
              ? { latest: laterOf(athensTime(new Date(at + 17 * 60_000).toISOString()), athensTime(new Date(at + 2 * 60_000).toISOString())) }
              : null;
            publish.mutate({
              origin_label: label(from as Pin, fromPoint),
              destination_label: label(to as Pin, toPoint),
              kind,
              ...(leavingNow
                ? { oneoff_date: athensDate(new Date(at + 2 * 60_000)) }
                : kind === 'oneoff'
                  ? { oneoff_date: oneoffDate }
                  : { weekdays: [...weekdays] }),
              depart_earliest: leavingNow ? athensTime(new Date(at + 2 * 60_000).toISOString()) : earliest,
              seats,
            });
          }}
          // Not until the groups have loaded: while they are loading, or if
          // they failed, the "who sees it" choice is not on screen and the
          // route would go out to everybody without the driver having seen
          // the question.
          disabled={
            !from || !to || (kind === 'recurring' && !weekdays.length) || noCar || noBootLeft || publish.isPending || !myGroups.isSuccess
          }
        />
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { gap: space.md, paddingBottom: 140 },
  label: { ...type.caption, color: colors.muted },
  days: { flexDirection: 'row', gap: 6 },
  // Group names are long, and a row of them runs off the screen otherwise.
  wrap: { flexWrap: 'wrap' },
  times: { flexDirection: 'row', gap: space.sm },
  seats: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  seatsLabel: { ...type.body, color: colors.ink },
  toggle: { ...type.label, color: colors.ink },
  hint: { ...type.caption, color: colors.muted, marginTop: space.xs },
  divider: { height: 1, backgroundColor: colors.line, marginVertical: space.sm },
});

// Where the stepper starts once a driver has said they want to set a figure for
// this trip. Until then nothing is shown and nothing is sent, so this is never
// mistaken for what their profile holds.
const defaultDetourKm = 10;

// Two weeks of dates for a one-off. The server takes up to three, but a
// fortnight of chips is already a long row, and a trip further out than that
// is rarely settled enough to offer.
const ONEOFF_DAYS = 14;

/** `n` Athens calendar dates, starting with `today`. */
function nextDates(today: string, n: number): string[] {
  const out = [today];
  while (out.length < n) out.push(nextDate(out[out.length - 1]));
  return out;
}

// A repeated query parameter arrives as an array; the first is what was meant.
function first(value: string | string[] | undefined): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}

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

/**
 * The latest departure for «Φεύγω τώρα»: a quarter of an hour on, unless that
 * crosses midnight, which a one-day window cannot, and then the last minute of
 * the day.
 */
function laterOf(latest: string, earliest: string): string {
  return latest > earliest ? latest : '23:59';
}

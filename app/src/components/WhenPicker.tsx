// When you want to leave.
//
// The screen used to decide this on your behalf — always "now, for the next two
// hours" — which is the right default for a taxi and the wrong one for the
// journey this app exists for. A commute is decided the night before, and the
// question it answers is "who is going tomorrow at half seven", not "who is
// going in the next ninety minutes".
//
// Two rows rather than a date-and-time dialog, because that is the shape of the
// answer people actually have: a day, and a part of it. A minute-accurate
// picker would be asking for precision nobody has and the matcher does not
// want — it matches against a window either way.
//
// A day further on is a date picked from the next two weeks: a trip on
// Thursday used to wait until Wednesday to be asked for (QA 2026-10-16, U1).
// Other days that would do as well are a list of their own, each asked for
// separately; the first one a driver takes withdraws the rest.

import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';

import { useState } from 'react';

import { Chip, SegmentedControl } from './ui';
import type { IconName } from './Icon';
import { colors, space, type } from '../design/tokens';
import { calendarDay } from '../design/format';
import { athensDate, bandIsOpen, dateOf, departureWindow, nextDate, type Band, type Day } from '../design/when';

type Choice = 'today' | 'tomorrow' | 'other';
const DAYS: Choice[] = ['today', 'tomorrow', 'other'];

/** How far ahead a date can be picked, in days after today. */
const AHEAD = 13;
/** How many other days one journey can be asked for on. The server allows seven in all. */
export const MAX_EXTRA_DAYS = 6;

/** The dates from `first` days after today to AHEAD days after it. */
function datesAhead(now: Date, first: number): Day[] {
  const out: Day[] = [];
  let d = athensDate(now);
  for (let i = 0; i <= AHEAD; i++) {
    if (i >= first) out.push(d as Day);
    d = nextDate(d);
  }
  return out;
}
const BANDS: Band[] = ['now', 'morning', 'midday', 'afternoon', 'evening'];

const BAND_ICON: Record<Band, IconName> = {
  now: 'flash-outline',
  morning: 'cafe-outline',
  midday: 'sunny-outline',
  afternoon: 'partly-sunny-outline',
  evening: 'moon-outline',
};

export function WhenPicker({
  day,
  band,
  onChange,
  extra,
  onExtraChange,
  now,
  testID,
}: {
  day: Day;
  band: Band;
  onChange: (next: { day: Day; band: Band }) => void;
  /** Other days that would do as well, as dates. Shown only with onExtraChange. */
  extra?: string[];
  onExtraChange?: (dates: string[]) => void;
  /** Injectable so the summary can be asserted without moving the clock. */
  now?: Date;
  testID?: string;
}) {
  const { t } = useTranslation();
  const at = now ?? new Date();
  const others = extra ?? [];
  const [more, setMore] = useState(others.length > 0);
  const choice: Choice = day === 'today' || day === 'tomorrow' ? day : 'other';

  // "Now" is only a thing today. Picking a later day while it is selected has
  // to land somewhere real rather than on a window that starts in the past.
  // The day itself leaves the list of other days, if it was on it.
  const pickDay = (next: Day) => {
    onChange({ day: next, band: next !== 'today' && band === 'now' ? 'morning' : band });
    const date = dateOf(next, at);
    if (others.includes(date)) onExtraChange?.(others.filter((d) => d !== date));
  };
  const pickChoice = (next: Choice) => {
    if (next === choice) return;
    pickDay(next === 'other' ? datesAhead(at, 2)[0] : next);
  };
  const toggleExtra = (date: string) =>
    onExtraChange?.(
      others.includes(date) ? others.filter((d) => d !== date) : [...others, date].sort(),
    );

  const window = departureWindow(day, band, at);
  const hhmm = (d: Date) =>
    new Intl.DateTimeFormat('el-GR', {
      timeZone: 'Europe/Athens',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    }).format(d);

  // A band whose last minute has gone is not a ride anybody can take, and
  // offering it produces a request the matcher will never fill.
  const bands = BANDS.filter((b) => bandIsOpen(day, b, at) && !(b === 'now' && day !== 'today'));
  const main = dateOf(day, at);

  return (
    <View testID={testID} style={styles.box}>
      <Text style={styles.label}>{t('search.when')}</Text>

      <SegmentedControl
        testID={testID ? `${testID}-day` : undefined}
        options={DAYS.map((d) => ({ value: d, label: t(`when.${d}`) }))}
        value={choice}
        onChange={pickChoice}
      />

      {choice === 'other' ? (
        <View style={styles.row}>
          {datesAhead(at, 2).map((d) => (
            <Chip
              key={d}
              testID={testID ? `${testID}-date-${d}` : undefined}
              fit
              label={calendarDay(d)}
              selected={day === d}
              onPress={() => pickDay(d)}
            />
          ))}
        </View>
      ) : null}

      <View style={styles.row}>
        {bands.map((b) => {
          return (
            <Chip
              key={b}
              testID={testID ? `${testID}-band-${b}` : undefined}
              icon={BAND_ICON[b]}
              // Five do not fit one row at phone width («Μεσημέρι» ran into
              // «Απόγευμα»), so they wrap into an even grid: three and two,
              // or two and two.
              minWidth={bands.length > 4 ? '30%' : bands.length === 4 ? '45%' : undefined}
              label={t(`when.${b}`)}
              selected={band === b}
              onPress={() => onChange({ day, band: b })}
            />
          );
        })}
      </View>

      {/* The window in plain figures. What the app is about to ask for should
          not have to be inferred from which chip is dark. */}
      <Text testID={testID ? `${testID}-summary` : undefined} style={styles.summary}>
        {t('when.summary', {
          day: choice === 'other' ? calendarDay(day) : t(`when.${day}`).toLowerCase(),
          from: hhmm(window.start),
          to: hhmm(window.end),
        })}
      </Text>

      {onExtraChange ? (
        <View style={styles.box}>
          <Chip
            testID={testID ? `${testID}-more` : undefined}
            fit
            icon="calendar-outline"
            label={t('when.more')}
            selected={more}
            onPress={() => {
              if (more) onExtraChange([]);
              setMore(!more);
            }}
          />
          {more ? (
            <>
              <View style={styles.row}>
                {datesAhead(at, 1)
                  .filter((d) => d !== main)
                  .map((d) => {
                    const on = others.includes(d);
                    return (
                      <Chip
                        key={d}
                        testID={testID ? `${testID}-extra-${d}` : undefined}
                        fit
                        label={calendarDay(d)}
                        selected={on}
                        disabled={!on && others.length >= MAX_EXTRA_DAYS}
                        onPress={() => toggleExtra(d)}
                      />
                    );
                  })}
              </View>
              <Text style={styles.summary}>{t('when.moreHint')}</Text>
            </>
          ) : null}
        </View>
      ) : null}
    </View>
  );
}

/** Re-exported so the screen has one import for the whole question. */
export { departureWindow, athensDate };

const styles = StyleSheet.create({
  box: { gap: space.sm },
  label: { ...type.label, color: colors.ink },
  row: { flexDirection: 'row', flexWrap: 'wrap', gap: space.xs },
  summary: { ...type.caption, color: colors.muted },
});

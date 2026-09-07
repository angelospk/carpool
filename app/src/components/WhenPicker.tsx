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

import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';

import { Chip } from './ui';
import { colors, space, type } from '../design/tokens';
import { athensDate, bandIsOpen, departureWindow, type Band, type Day } from '../design/when';

const DAYS: Day[] = ['today', 'tomorrow'];
const BANDS: Band[] = ['now', 'morning', 'midday', 'afternoon', 'evening'];

const BAND_GLYPH: Record<Band, string> = {
  now: '⚡',
  morning: '🌅',
  midday: '☀️',
  afternoon: '🌤️',
  evening: '🌙',
};

export function WhenPicker({
  day,
  band,
  onChange,
  now,
  testID,
}: {
  day: Day;
  band: Band;
  onChange: (next: { day: Day; band: Band }) => void;
  /** Injectable so the summary can be asserted without moving the clock. */
  now?: Date;
  testID?: string;
}) {
  const { t } = useTranslation();
  const at = now ?? new Date();

  // "Now" is only a thing today. Picking tomorrow while it is selected has to
  // land somewhere real rather than on a window that starts in the past.
  const pickDay = (next: Day) =>
    onChange({ day: next, band: next === 'tomorrow' && band === 'now' ? 'morning' : band });

  const window = departureWindow(day, band, at);
  const hhmm = (d: Date) =>
    new Intl.DateTimeFormat('el-GR', {
      timeZone: 'Europe/Athens',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    }).format(d);

  return (
    <View testID={testID} style={styles.box}>
      <Text style={styles.label}>{t('search.when')}</Text>

      <View style={styles.row}>
        {DAYS.map((d) => (
          <Chip
            key={d}
            testID={testID ? `${testID}-day-${d}` : undefined}
            label={t(`when.${d}`)}
            selected={day === d}
            onPress={() => pickDay(d)}
          />
        ))}
      </View>

      <View style={styles.row}>
        {BANDS.map((b) => {
          // A band whose last minute has gone is not a ride anybody can take,
          // and offering it produces a request the matcher will never fill.
          if (!bandIsOpen(day, b, at)) return null;
          if (b === 'now' && day === 'tomorrow') return null;
          return (
            <Chip
              key={b}
              testID={testID ? `${testID}-band-${b}` : undefined}
              glyph={BAND_GLYPH[b]}
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
          day: t(`when.${day}`).toLowerCase(),
          from: hhmm(window.start),
          to: hhmm(window.end),
        })}
      </Text>
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

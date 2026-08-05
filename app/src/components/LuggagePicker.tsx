// How much you are bringing, or how much you will take.
//
// It is the same control on both sides of the app and deliberately so: the
// passenger picks what is in their hands and the driver picks the largest thing
// they will carry, and the two are compared. The order is the meaning — a
// driver who takes a suitcase takes a cabin bag — so the chips are laid out
// smallest first and read as a scale rather than as a set of options.
//
// This one is not a preference. Preferences describe; this decides who is even
// offered the trip, which is why the hint under it says so.

import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';

import { LUGGAGE_SIZES, type LuggageSize } from '../api/endpoints';
import { Chip } from './ui';
import { colors, space, type } from '../design/tokens';

export function LuggagePicker({
  value,
  onChange,
  count,
  onCountChange,
  /** The driver's side asks a different question from the passenger's. */
  asDriver = false,
  testID,
}: {
  value: LuggageSize;
  onChange: (size: LuggageSize) => void;
  /** How many of that size. Omit to ask about size alone. */
  count?: number;
  onCountChange?: (n: number) => void;
  asDriver?: boolean;
  testID?: string;
}) {
  const { t } = useTranslation();

  const counts = asDriver ? DRIVER_COUNTS : PASSENGER_COUNTS;
  const asking = count !== undefined && onCountChange !== undefined;

  // Choosing a size has to leave a usable pair behind: the two columns
  // constrain each other in the database, and a size with a count of zero is a
  // row the server will not take.
  const pick = (size: LuggageSize) => {
    onChange(size);
    if (!onCountChange) return;
    if (size === 'none') onCountChange(0);
    else if (!count) onCountChange(1);
  };

  return (
    <View testID={testID} style={styles.box}>
      <Text style={styles.label}>{t(asDriver ? 'luggage.driverLabel' : 'luggage.label')}</Text>
      <View style={styles.row}>
        {LUGGAGE_SIZES.map((size) => (
          <Chip
            key={size}
            testID={testID ? `${testID}-${size}` : undefined}
            label={t(`luggage.${size}`)}
            selected={value === size}
            onPress={() => pick(size)}
          />
        ))}
      </View>

      {/* Nothing has no number. Asking "how many nothings" would invite an
          answer the server refuses, and the pair is checked there. */}
      {asking && value !== 'none' ? (
        <>
          <Text style={styles.label}>
            {t(asDriver ? 'luggage.driverCountLabel' : 'luggage.countLabel')}
          </Text>
          <View style={styles.row}>
            {counts.map((n) => (
              <Chip
                key={n}
                testID={testID ? `${testID}-count-${n}` : undefined}
                label={String(n)}
                selected={count === n}
                onPress={() => onCountChange(n)}
              />
            ))}
          </View>
        </>
      ) : null}

      <Text style={styles.hint}>
        {t(asDriver ? 'luggage.driverHint' : 'luggage.hint')}
      </Text>
    </View>
  );
}

// What a passenger can declare, and what a boot can be said to hold. Both match
// the CHECK constraints in migration 00026: somebody with five things is moving
// house, and eight places is a van with the seats down.
const PASSENGER_COUNTS = [1, 2, 3, 4] as const;
const DRIVER_COUNTS = [1, 2, 3, 4, 5, 6, 7, 8] as const;

const styles = StyleSheet.create({
  box: { gap: space.xs },
  label: { ...type.body, color: colors.ink },
  row: { flexDirection: 'row', flexWrap: 'wrap', gap: space.xs },
  hint: { ...type.caption, color: colors.muted },
});

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
  /** The driver's side asks a different question from the passenger's. */
  asDriver = false,
  testID,
}: {
  value: LuggageSize;
  onChange: (size: LuggageSize) => void;
  asDriver?: boolean;
  testID?: string;
}) {
  const { t } = useTranslation();

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
            onPress={() => onChange(size)}
          />
        ))}
      </View>
      <Text style={styles.hint}>
        {t(asDriver ? 'luggage.driverHint' : 'luggage.hint')}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  box: { gap: space.xs },
  label: { ...type.body, color: colors.ink },
  row: { flexDirection: 'row', flexWrap: 'wrap', gap: space.xs },
  hint: { ...type.caption, color: colors.muted },
});

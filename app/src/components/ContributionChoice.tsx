// What happens about the fuel.
//
// The app used to decide this. It computed a figure and both sides saw it,
// which is the platform setting the price however transparently it showed its
// working — and that is the thing άρθρο 12 ν. 4530/2018 makes relevant to how
// an intermediary is characterised. Our own terms already called the
// contribution a private arrangement between two people; this is the screen
// catching up with them.
//
// There are two answers, and they are the two a driver actually gives:
//
//   · work it out for me — the fuel formula suggests a figure, capped at the
//     driver's own share so that nobody comes out ahead;
//   · leave it open — nothing is suggested at all, and whatever passes between
//     the two people passes between the two people.
//
// Under the first, and only under it, the driver may ask for less than their
// share. That is a fraction of a number the formula produced, so it is shown
// beneath the choice it belongs to rather than beside it as a fifth option.
//
// BlaBlaCar's Greek terms describe the same shape: "το ποσό της Συνεισφοράς στα
// Έξοδα καθορίζεται από εσάς, ως Οδηγό", with the platform suggesting and
// restricting how far it can be adjusted.

import { Pressable, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';

import { type PricingMode } from '../api/endpoints';
import { Chip } from './ui';
import { colors, radius, space, type } from '../design/tokens';

/** The fractions offered under the formula. Not a free field: the figure varies
    with each passenger's own distance, so there is no single euro amount to
    type. */
const SHARES = [1, 0.75, 0.5, 0.25] as const;

export function ContributionChoice({
  mode,
  onModeChange,
  share,
  onShareChange,
  testID,
}: {
  mode: PricingMode;
  onModeChange: (mode: PricingMode) => void;
  share: number;
  onShareChange: (share: number) => void;
  testID?: string;
}) {
  const { t } = useTranslation();
  const auto = mode === 'formula';
  // There is a third mode in the API — `free`, a driver who asks for nothing —
  // and it is deliberately not offered here: the two answers are the two a
  // driver gives. A route that somehow holds it reads as neither option rather
  // than as the open contribution, which is a different promise.
  const open = mode === 'free_contribution';

  return (
    <View testID={testID} style={styles.box}>
      <Text style={styles.label}>{t('contribution.label')}</Text>

      <Option
        testID={testID ? `${testID}-auto` : undefined}
        title={t('contribution.auto')}
        hint={t('contribution.autoHint')}
        selected={auto}
        onPress={() => onModeChange('formula')}
      />
      <Option
        testID={testID ? `${testID}-free` : undefined}
        title={t('contribution.free')}
        hint={t('contribution.freeHint')}
        selected={open}
        onPress={() => onModeChange('free_contribution')}
      />

      {/* Only under the formula, because a fraction of nothing is nothing. The
          driver's last choice is kept by whoever owns this state, so switching
          away and back does not silently reset them to the whole share. */}
      {auto ? (
        <View style={styles.less}>
          <Text style={styles.lessLabel}>{t('contribution.less')}</Text>
          <View style={styles.row}>
            {SHARES.map((s) => (
              <Chip
                key={s}
                testID={testID ? `${testID}-${Math.round(s * 100)}` : undefined}
                label={t(`contribution.share.${Math.round(s * 100)}`)}
                selected={share === s}
                onPress={() => onShareChange(s)}
              />
            ))}
          </View>
          <Text style={styles.hint}>{t('contribution.hint')}</Text>
        </View>
      ) : null}
    </View>
  );
}

/** One of the two answers, with the sentence that says what it means. */
function Option({
  title,
  hint,
  selected,
  onPress,
  testID,
}: {
  title: string;
  hint: string;
  selected: boolean;
  onPress: () => void;
  testID?: string;
}) {
  return (
    <Pressable
      testID={testID}
      accessibilityRole="radio"
      // `aria-checked`, not `accessibilityState`. Measured in a browser: this
      // whole app's `accessibilityState` was being dropped by react-native-web,
      // which forwards `aria-*` and nothing else — so both options announced as
      // "Αυτόματο μερίδιο, radio button" with no way to tell which was chosen.
      // React Native normalises `aria-checked` back into accessibilityState, so
      // this is the one spelling that reaches a screen reader on both.
      aria-checked={selected}
      onPress={onPress}
      style={[styles.option, selected && styles.optionOn]}
    >
      <View style={[styles.dot, selected && styles.dotOn]} />
      <View style={styles.optionText}>
        <Text style={[styles.optionTitle, selected && styles.optionTitleOn]}>{title}</Text>
        <Text style={styles.optionHint}>{hint}</Text>
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  box: { gap: space.sm },
  label: { ...type.body, color: colors.ink },
  option: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: space.md,
    padding: space.md,
    borderRadius: radius.control,
    backgroundColor: colors.surface,
  },
  optionOn: { backgroundColor: colors.surface },
  dot: {
    width: 18,
    height: 18,
    borderRadius: 9,
    borderWidth: 2,
    borderColor: colors.line,
    marginTop: 2,
  },
  dotOn: { borderColor: colors.ink, borderWidth: 6 },
  optionText: { flex: 1, gap: 2 },
  optionTitle: { ...type.label, color: colors.muted },
  optionTitleOn: { color: colors.ink },
  optionHint: { ...type.caption, color: colors.muted },
  less: { gap: space.xs, marginTop: space.xs },
  lessLabel: { ...type.caption, color: colors.ink },
  row: { flexDirection: 'row', flexWrap: 'wrap', gap: space.xs },
  hint: { ...type.caption, color: colors.muted },
});

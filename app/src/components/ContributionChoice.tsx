// What the driver asks for, out of what the fuel comes to.
//
// The app used to decide this. It computed a figure and both sides saw it,
// which is the platform setting the price however transparently it showed its
// working — and that is the thing άρθρο 12 ν. 4530/2018 makes relevant to how
// an intermediary is characterised. Our own terms already called the
// contribution a private arrangement between two people; this is the screen
// catching up with them.
//
// So the computed share became a ceiling and this is the driver choosing under
// it. The same number comes out most of the time, because most people will
// leave it at all of it — but it is theirs to leave.
//
// BlaBlaCar's Greek terms describe the same shape: "το ποσό της Συνεισφοράς στα
// Έξοδα καθορίζεται από εσάς, ως Οδηγό", with the platform suggesting and
// restricting how far it can be adjusted.

import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';

import { Chip } from './ui';
import { colors, space, type } from '../design/tokens';

/** The fractions offered. Not a free field: the figure varies with each
    passenger's own distance, so there is no single euro amount to type. */
const SHARES = [1, 0.75, 0.5, 0.25] as const;

export function ContributionChoice({
  value,
  onChange,
  testID,
}: {
  value: number;
  onChange: (share: number) => void;
  testID?: string;
}) {
  const { t } = useTranslation();

  return (
    <View testID={testID} style={styles.box}>
      <Text style={styles.label}>{t('contribution.label')}</Text>
      <View style={styles.row}>
        {SHARES.map((share) => (
          <Chip
            key={share}
            testID={testID ? `${testID}-${Math.round(share * 100)}` : undefined}
            label={t(`contribution.share.${Math.round(share * 100)}`)}
            selected={value === share}
            onPress={() => onChange(share)}
          />
        ))}
      </View>
      <Text style={styles.hint}>{t('contribution.hint')}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  box: { gap: space.xs },
  label: { ...type.body, color: colors.ink },
  row: { flexDirection: 'row', flexWrap: 'wrap', gap: space.xs },
  hint: { ...type.caption, color: colors.muted },
});

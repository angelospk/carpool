// What there is instead of a map.
//
// A deployment without a tile source draws this rather than a blank grey box or
// a crash. It is not an error state — the app worked without a map until now
// and still does — so it says what is missing in the one place somebody
// configuring the app will see it, and nothing at all to everybody else.

import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';

import { colors, radius, space, type } from '../design/tokens';

export function MapFallback({ testID }: { testID?: string }) {
  const { t } = useTranslation();
  return (
    <View testID={testID} style={styles.box}>
      <Text style={styles.label}>{t('map.unconfigured')}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  box: {
    backgroundColor: colors.sunken,
    borderRadius: radius.inner,
    padding: space.lg,
    alignItems: 'center',
  },
  label: { ...type.caption, color: colors.muted, textAlign: 'center' },
});

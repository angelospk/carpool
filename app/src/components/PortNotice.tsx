// Why this trip stops somewhere nobody typed.
//
// A driver who posts Athens to Mykonos gets a trip that ends at the port,
// because the part after that is a boat. Without a word of explanation the trip
// simply looks wrong — a short drive to a place that is not the destination —
// and the passenger's first assumption is that the app is broken.
//
// It also says what is not included, because the price is fuel over the
// distance driven and the car's ferry ticket is nobody's business but the
// person who buys it.

import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';

import { colors, radius, space, type } from '../design/tokens';

export function PortNotice({ endsAtPort }: { endsAtPort: boolean }) {
  const { t } = useTranslation();
  if (!endsAtPort) return null;

  return (
    <View testID="port-notice" style={styles.box}>
      <Text style={styles.title}>{t('trip.portTitle')}</Text>
      <Text style={styles.body}>{t('trip.portBody')}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  box: {
    backgroundColor: colors.sunken,
    borderRadius: radius.inner,
    borderLeftWidth: 3,
    borderLeftColor: colors.primary,
    padding: space.md,
    gap: space.xs,
  },
  title: { ...type.label, color: colors.ink },
  body: { ...type.caption, color: colors.muted },
});

import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';

import { api } from '../api/endpoints';
import { type Pin } from '../design/pin';
import { colors, radius, space, type } from '../design/tokens';

/**
 * Whether cars go this way on the chosen day, before anybody asks.
 *
 * A yes, once at least three different drivers do, and never how many: an
 * exact number moves by one when one driver leaves it, and that is a person.
 * Below three the server says "unknown" and this says nothing. Not "nobody" — the request below still works, and waits
 * for a route if there is none.
 */
export function Supply({ from, to, date }: { from: Pin | null; to: Pin | null; date: string }) {
  const { t } = useTranslation();
  const supply = useQuery({
    queryKey: ['supply', from?.lon, from?.lat, to?.lon, to?.lat, date],
    queryFn: () =>
      api.supply({
        origin_lon: (from as Pin).lon,
        origin_lat: (from as Pin).lat,
        destination_lon: (to as Pin).lon,
        destination_lat: (to as Pin).lat,
        date,
      }),
    enabled: !!from && !!to,
    // A count that is a minute old is still the answer; asking again on every
    // focus would spend the rate limit on nothing.
    staleTime: 60_000,
  });

  if (!from || !to || supply.data?.status !== 'several') return null;

  return (
    <View testID="supply" style={styles.wrap}>
      <View style={styles.dot} />
      <Text style={styles.text}>{t('search.supply')}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    backgroundColor: colors.sunken,
    borderRadius: radius.inner,
    paddingHorizontal: space.md,
    paddingVertical: space.sm,
  },
  dot: { width: 8, height: 8, borderRadius: 4, backgroundColor: colors.success },
  text: { ...type.label, color: colors.ink, flex: 1 },
});

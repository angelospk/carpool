// Roughly what this tends to cost, before anybody has been asked.
//
// It answers the question a first-time user has and the app could not answer at
// all: a figure only appeared once a driver had been asked, because it depends
// on their car and how many seats they published.
//
// Everything about how it is worded is deliberate. It is not an offer: no
// driver has seen this trip and nobody has agreed to anything, so it says
// "about", it names what it assumed, and it says plainly that the real figure
// arrives with a specific car. A screen that showed a confident number here
// would be describing a price for a service that nobody has offered.

import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';

import { api, type PlanningEstimate } from '../api/endpoints';
import { type Pin } from '../design/pin';
import { euros } from '../design/format';
import { colors, radius, space, type } from '../design/tokens';

export function Estimate({
  from,
  to,
  testID,
}: {
  from: Pin | null;
  to: Pin | null;
  testID?: string;
}) {
  const { t } = useTranslation();

  const { data, isError } = useQuery<PlanningEstimate>({
    // Keyed on the coordinates, so moving either pin asks again and a stale
    // figure never sits under a changed route.
    queryKey: ['estimate', from?.lon, from?.lat, to?.lon, to?.lat],
    queryFn: () =>
      api.estimate({
        origin_lon: (from as Pin).lon,
        origin_lat: (from as Pin).lat,
        destination_lon: (to as Pin).lon,
        destination_lat: (to as Pin).lat,
      }),
    enabled: !!from && !!to,
    retry: false,
  });

  // Silent when there is nothing to say. A trip that cannot be routed, or a
  // deployment without fuel prices, shows no figure rather than a fabricated
  // one — and not an error either, because nothing the person did was wrong.
  if (!from || !to || isError || !data) return null;

  return (
    <View testID={testID} style={styles.box}>
      <Text testID={testID ? `${testID}-headline` : undefined} style={styles.headline}>
        {t('estimate.about', { amount: euros(data.contribution_cents) })}
      </Text>
      {/* The upper bound, and only the upper bound. Nobody is upset by paying
          less than they expected; somebody quoted 12 € and asked for 25 € has
          been misled. A matching lower figure would make the pair five times
          wide, which reads as "nobody knows" and gets ignored. */}
      {data.ceiling_cents > data.contribution_cents ? (
        <Text testID={testID ? `${testID}-ceiling` : undefined} style={styles.detail}>
          {t('estimate.upTo', { amount: euros(data.ceiling_cents) })}
        </Text>
      ) : null}
      <Text style={styles.detail}>
        {t('estimate.basis', { km: data.route_km.toFixed(0), seats: data.assumptions.divisor - 1 })}
      </Text>
      {/* The sentence that keeps this from being a price. */}
      <Text testID={testID ? `${testID}-caveat` : undefined} style={styles.detail}>
        {t('estimate.notAnOffer')}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  box: {
    gap: space.xs,
    padding: space.md,
    backgroundColor: colors.sunken,
    borderRadius: radius.inner,
  },
  headline: { ...type.label, color: colors.ink },
  detail: { ...type.caption, color: colors.muted },
});

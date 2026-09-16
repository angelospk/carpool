// Why publishing is worth doing, on the screen where somebody decides not to.
//
// A driver with nothing published opens an empty form and has no way of knowing
// whether anybody would ride with them. The answer was in the requests table
// the whole time and only the people who made those requests could see it.
// "Three people are looking for Αλεξανδρούπολη → Ορεστιάδα this week" is the
// strongest argument this app has for filling the form in.
//
// Counts, never names. The endpoint sends a corridor and a number and nothing
// else, and each line here is one interpolated sentence rather than a row
// assembled from fields, so there is no place for a fourth value to appear if
// one ever arrives in the payload.

import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';

import { api } from '../api/endpoints';
import { useSession } from '../state/session';
import { colors, space, type } from '../design/tokens';

/** How many roads are worth reading before deciding. Past this the tail is
    corridors with one person on them, pushing the busy one off the screen. */
const SHOWN = 5;

export function Demand() {
  const { t } = useTranslation();
  // Same guard as useOpenRequests: the tabs mount before the session settles,
  // and without it this fires a request with no token on every cold start.
  const signedIn = useSession((s) => s.status === 'signedIn');

  const demand = useQuery({
    queryKey: ['demand'],
    queryFn: () => api.demand(),
    enabled: signedIn,
  });

  // Silence on failure, deliberately. This is an encouragement rather than part
  // of publishing, and an error box over a form that works would make the
  // screen look broken when nothing that matters is.
  const corridors = demand.data?.corridors ?? [];
  if (corridors.length === 0) return null;

  return (
    <View testID="demand" style={styles.wrap}>
      <Text style={styles.heading}>{t('demand.title')}</Text>
      {corridors.slice(0, SHOWN).map((c, i) => (
        <Text key={`${c.origin_label}→${c.destination_label}`} testID={`demand-${i}`} style={styles.line}>
          {t('demand.people', {
            count: c.people,
            from: c.origin_label,
            to: c.destination_label,
          })}
        </Text>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: 2 },
  heading: { ...type.caption, color: colors.muted, marginLeft: space.xs },
  line: { ...type.body, color: colors.ink, marginLeft: space.xs },
});

// Somewhere you go often, one tap from asking again.
//
// The ranking lives in src/design/suggestions.ts, which is where the arguments
// about what counts as the same journey belong. This is the part that draws
// them and hands both pins to the search screen, so the form opens filled in
// rather than empty for the fifth time.

import { useRef } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { useRouter } from 'expo-router';

import { api } from '../api/endpoints';
import { useSession } from '../state/session';
import { Card } from './ui';
import { suggestRoutes } from '../design/suggestions';
import { colors, space, type } from '../design/tokens';

export function Suggestions() {
  const { t } = useTranslation();
  const router = useRouter();

  // Both share react-query's cache with the home screen, so this section costs
  // no request of its own.
  // The search tab stays mounted, so the preset it receives has to differ every
  // time: otherwise the second press of the same suggestion is indistinguishable
  // from a re-render of the first, and quietly does nothing.
  const presses = useRef(0);

  // The same guard the home screen carries, for the same reason: this section
  // mounts inside the tab list before the session has settled, and an observer
  // without the guard fires its own request without a token and gets a 401.
  // Sharing the cache key is not sharing the `enabled` flag.
  const signedIn = useSession((s) => s.status === 'signedIn');

  const past = useQuery({
    queryKey: ['trips', 'past'],
    queryFn: () => api.pastTrips(),
    enabled: signedIn,
  });
  const upcoming = useQuery({
    queryKey: ['trips', 'upcoming'],
    queryFn: () => api.upcomingTrips(),
    enabled: signedIn,
  });

  const suggestions = suggestRoutes(past.data?.trips ?? [], upcoming.data?.trips ?? []);
  if (suggestions.length === 0) return null;

  return (
    <View testID="suggestions" style={styles.wrap}>
      <Text style={styles.heading}>{t('home.suggestions')}</Text>
      {suggestions.map((s) => (
        <Card
          key={s.key}
          testID={`suggestion-${s.key}`}
          onPress={() =>
            router.push({
              pathname: '/(tabs)/search',
              params: {
                // The key and a press counter travel with the pins, so a second
                // press of the same suggestion is not mistaken for a re-render
                // of the first.
                preset: `${s.key}#${(presses.current += 1)}`,
                from_lon: String(s.from.lon),
                from_lat: String(s.from.lat),
                from_label: s.from.label,
                to_lon: String(s.to.lon),
                to_lat: String(s.to.lat),
                to_label: s.to.label,
              },
            })
          }
        >
          <Text style={styles.route} numberOfLines={1}>
            {s.from.label} → {s.to.label}
          </Text>
          <Text style={styles.hint}>{t('home.suggestionHint')}</Text>
        </Card>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: space.sm, marginTop: space.lg },
  heading: { ...type.caption, color: colors.muted, marginLeft: space.xs },
  route: { ...type.body, color: colors.ink },
  hint: { ...type.caption, color: colors.muted, marginTop: 2 },
});

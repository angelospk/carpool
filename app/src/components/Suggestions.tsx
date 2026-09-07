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

  const past = useQuery({ queryKey: ['trips', 'past'], queryFn: () => api.pastTrips() });
  const upcoming = useQuery({ queryKey: ['trips', 'upcoming'], queryFn: () => api.upcomingTrips() });

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

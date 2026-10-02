import { useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';

import { api } from '../api/endpoints';
import { ApiError } from '../api/client';
import { Avatar, Badge, Button } from './ui';
import { colors, radius, space, type } from '../design/tokens';

/**
 * One person to rate, after a ride.
 *
 * The stars are picked first and sent on purpose: the old button sent five
 * stars on the first tap, which made every rating in the app a five and the
 * average say nothing. The ratee travels in the mutation itself rather than
 * being looked up when it runs, so a driver with two passengers rates the one
 * whose row they touched.
 *
 * "Already rated" from the server is the same end state as a rating accepted
 * now, so it reads as done rather than as a failure to retry.
 */
export function RatingRow({
  tripId,
  person,
  rated,
  onRated,
}: {
  tripId: string;
  person: { id: string; name: string; photo_url: string | null };
  rated: boolean;
  onRated: () => void;
}) {
  const { t } = useTranslation();
  const [stars, setStars] = useState(0);
  // Done if the server says so on any fetch, or if a rating was accepted here.
  // Reading `rated` only on mount missed a rating given from another device.
  const [sent, setSent] = useState(false);
  const done = rated || sent;
  const [failed, setFailed] = useState(false);
  const id = `rate-${person.id}`;

  const send = useMutation({
    mutationFn: (v: { stars: number; rateeId: string }) => api.rateTrip(tripId, v.stars, v.rateeId),
    onSuccess: () => {
      setSent(true);
      onRated();
    },
    onError: (e) => {
      if (e instanceof ApiError && e.code === 'already_rated') {
        setSent(true);
        onRated();
        return;
      }
      // The chosen stars stay picked, so trying again is one tap.
      setFailed(true);
    },
  });

  return (
    <View style={styles.row}>
      <View style={styles.who}>
        <Avatar name={person.name} photoUrl={person.photo_url} size={36} />
        <Text style={styles.name} numberOfLines={1}>
          {person.name}
        </Text>
        {done ? <Badge testID={`${id}-done`} label={t('trip.ratedDone')} tone="success" /> : null}
      </View>

      {done ? null : (
        <>
          <View style={styles.stars} accessibilityRole="radiogroup">
            {[1, 2, 3, 4, 5].map((n) => (
              <Pressable
                key={n}
                testID={`${id}-star-${n}`}
                accessibilityRole="radio"
                accessibilityLabel={t('trip.rateStars', { count: n })}
                aria-checked={stars === n}
                hitSlop={6}
                onPress={() => {
                  setStars(n);
                  setFailed(false);
                }}
                style={({ pressed }) => [styles.star, pressed && styles.pressed]}
              >
                <Text style={[styles.starGlyph, n <= stars && styles.starOn]}>★</Text>
              </Pressable>
            ))}
          </View>
          {failed ? <Text style={styles.error}>{t('common.error')}</Text> : null}
          <Button
            testID={`${id}-send`}
            label={t('trip.rateSend')}
            variant="secondary"
            disabled={stars === 0 || send.isPending}
            onPress={() => send.mutate({ stars, rateeId: person.id })}
          />
        </>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    gap: space.sm,
    backgroundColor: colors.sunken,
    borderRadius: radius.inner,
    padding: space.md,
  },
  who: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
  name: { ...type.body, color: colors.ink, flex: 1 },
  stars: { flexDirection: 'row', gap: space.xs, justifyContent: 'center' },
  star: { paddingHorizontal: space.xs, minWidth: 44, minHeight: 44, alignItems: 'center', justifyContent: 'center' },
  starGlyph: { fontSize: 32, color: colors.line },
  starOn: { color: colors.accent },
  pressed: { opacity: 0.6 },
  error: { ...type.caption, color: colors.danger, textAlign: 'center' },
});

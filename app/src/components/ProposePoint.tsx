// Suggesting a spot the automatic list does not know about.
//
// The harvested landmarks are thin outside the towns and wrong inside the
// cities, and the person who knows where a car can actually wait in Φέρες is
// somebody who waits there. So they can say so.
//
// It is offered whether the list is empty or full, because a wrong list is as
// worth correcting as an absent one — Θεσσαλονίκη's three entries are all
// coach-ticket agencies, and somebody who knows that has nowhere to put it.
//
// What it does not do is pretend. A suggestion is not a meeting point until
// somebody has looked at it, and this says so plainly rather than showing it in
// the list greyed out, where it would look like something that could be picked.

import { useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';

import { api, type PointProposal } from '../api/endpoints';
import { ApiError } from '../api/client';
import { type Pin } from '../design/pin';
import { Button, Field } from './ui';
import { colors, radius, space, type } from '../design/tokens';

/** The kinds the server accepts, in the order they are worth suggesting. */
const KINDS = ['bus_station', 'train_station', 'port', 'fuel', 'parking', 'other'] as const;

export function ProposePoint({ place, testID }: { place: Pin; testID?: string }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState('');
  const [kind, setKind] = useState<string>('other');
  const [sent, setSent] = useState<PointProposal | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Made once per attempt rather than per render, so a retry after a timeout
  // carries the same key and the server treats it as the same suggestion.
  const [key] = useState(() => `${place.id}-${Date.now()}-${Math.random().toString(36).slice(2)}`);

  const propose = useMutation({
    mutationFn: () =>
      api.proposeMeetingPoint({
        place_id: place.id!,
        name,
        kind,
        // The settlement's own coordinate. Picking a point on a map is the
        // right way to do this and there is no map yet; a moderator sees the
        // name and can place it properly, which is what review is for.
        lon: place.lon,
        lat: place.lat,
        idempotency_key: key,
      }),
    onSuccess: (p) => {
      setSent(p);
      setError(null);
    },
    onError: (e) => {
      if (e instanceof ApiError) {
        // Each of these is something the person can do something about.
        const known: Record<string, string> = {
          too_many: t('places.proposeTooMany'),
          duplicate_point: t('places.proposeDuplicate'),
          invalid_name: t('places.proposeBadName'),
          too_far: t('places.proposeTooFar'),
        };
        return setError(known[e.code] ?? t('common.error'));
      }
      setError(t('common.error'));
    },
  });

  if (sent) {
    return (
      <Text testID={testID ? `${testID}-sent` : undefined} style={styles.sent}>
        {t('places.proposeSent')}
      </Text>
    );
  }

  if (!open) {
    return (
      <Text
        testID={testID ? `${testID}-open` : undefined}
        accessibilityRole="button"
        style={styles.link}
        onPress={() => setOpen(true)}
      >
        {t('places.proposeOpen')}
      </Text>
    );
  }

  return (
    <View testID={testID} style={styles.box}>
      <Field
        testID={testID ? `${testID}-name` : undefined}
        label={t('places.proposeName')}
        value={name}
        onChangeText={setName}
        placeholder={t('places.proposeNamePlaceholder')}
      />

      <View style={styles.kinds}>
        {KINDS.map((k) => (
          <Text
            key={k}
            testID={testID ? `${testID}-kind-${k}` : undefined}
            accessibilityRole="radio"
            accessibilityState={{ selected: kind === k }}
            onPress={() => setKind(k)}
            style={[styles.kind, kind === k && styles.kindChosen]}
          >
            {t(`places.point.${k}`)}
          </Text>
        ))}
      </View>

      {error ? <Text style={styles.error}>{error}</Text> : null}
      <Text style={styles.hint}>{t('places.proposeHint')}</Text>

      <Button
        testID={testID ? `${testID}-submit` : undefined}
        label={t('places.proposeSubmit')}
        disabled={name.trim().length < 2 || propose.isPending}
        onPress={() => propose.mutate()}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  box: { gap: space.sm, marginTop: space.sm },
  link: { ...type.caption, color: colors.primary, marginTop: space.sm },
  sent: { ...type.caption, color: colors.muted, marginTop: space.sm },
  kinds: { flexDirection: 'row', flexWrap: 'wrap', gap: space.xs },
  kind: {
    ...type.caption,
    color: colors.muted,
    paddingVertical: space.xs,
    paddingHorizontal: space.sm,
    borderRadius: radius.pill,
    backgroundColor: colors.sunken,
  },
  kindChosen: { color: colors.surface, backgroundColor: colors.primary },
  hint: { ...type.caption, color: colors.muted },
  error: { ...type.caption, color: colors.danger },
});

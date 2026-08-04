// Where exactly, once a town has been chosen.
//
// A town is not an answer to "where do I pick you up". This offers the
// landmarks OSM already carries for it — the ΚΤΕΛ, the station, the port, the
// petrol station — and nothing else. Four settlements in five have none, and
// where that is true it says so rather than inventing a plausible coordinate:
// being findable is the only promise a meeting point makes.

import { useQuery } from '@tanstack/react-query';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';

import { api, type MeetingPoint } from '../api/endpoints';
import { type Pin } from '../design/pin';
import { colors, radius, space, type } from '../design/tokens';

export function MeetingPointField({
  place,
  value,
  onChange,
  testID,
}: {
  /** The chosen settlement, or null while none is. */
  place: Pin | null;
  value: MeetingPoint | null;
  onChange: (p: MeetingPoint | null) => void;
  testID?: string;
}) {
  const { t } = useTranslation();

  const points = useQuery({
    queryKey: ['meeting-points', place?.id],
    queryFn: () => api.meetingPoints(place!.id!),
    enabled: !!place?.id,
  });

  // No settlement means the query never ran, so there is nothing to show and
  // nothing pending. There is no separate guard for it: one would be a line no
  // test could tell the absence of.
  if (points.isPending || !points.data) return null;

  // Nothing tagged anywhere near. Saying so is the point: the alternative is a
  // list of one invented entry that sends somebody to a field.
  if (!points.data?.length) {
    return (
      <Text testID={testID ? `${testID}-none` : undefined} style={styles.none}>
        {t('places.noMeetingPoints')}
      </Text>
    );
  }

  return (
    <View testID={testID} style={styles.list}>
      <Text style={styles.label}>{t('places.whereExactly')}</Text>
      {points.data.map((p) => {
        const chosen = value?.id === p.id;
        return (
          <Pressable
            key={p.id}
            testID={testID ? `${testID}-${p.id}` : undefined}
            onPress={() => onChange(chosen ? null : p)}
            style={[styles.row, chosen && styles.chosen]}
          >
            <Text style={styles.name}>{p.name}</Text>
            <Text style={styles.kind}>{t(`places.point.${p.kind}`)}</Text>
          </Pressable>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  list: { gap: space.xs, marginTop: space.sm },
  label: { ...type.caption, color: colors.muted },
  row: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: space.sm,
    paddingHorizontal: space.md,
    borderRadius: radius.inner,
    backgroundColor: colors.sunken,
  },
  chosen: { backgroundColor: colors.primary },
  name: { ...type.body, color: colors.ink, flexShrink: 1 },
  kind: { ...type.caption, color: colors.muted, marginLeft: space.sm },
  none: { ...type.caption, color: colors.muted, marginTop: space.sm },
});

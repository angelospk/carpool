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
import { ProposePoint } from './ProposePoint';
import { border, colors, radius, space, type } from '../design/tokens';

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

  // A coordinate somebody typed belongs to no settlement: nothing to look up,
  // and nothing to suggest a spot for either, since a suggestion has to be
  // filed against a place.
  if (!place?.id) return null;
  if (points.isPending || !points.data) return null;

  // Nothing tagged anywhere near. Saying so is the point: the alternative is a
  // list of one invented entry that sends somebody to a field. The offer to
  // suggest one is what turns that from a dead end into a question.
  if (!points.data?.length) {
    return (
      <View>
        <Text testID={testID ? `${testID}-none` : undefined} style={styles.none}>
          {t('places.noMeetingPoints')}
        </Text>
        <ProposePoint place={place} testID={testID ? `${testID}-propose` : undefined} />
      </View>
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
      {/* Offered even when the list is full: Θεσσαλονίκη's three entries are
          all coach-ticket agencies, and somebody who knows that needs
          somewhere to put it. */}
      <ProposePoint place={place} testID={testID ? `${testID}-propose` : undefined} />
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
    minHeight: 44,
    paddingVertical: space.sm,
    paddingHorizontal: space.md + border.selected - border.rest,
    borderRadius: radius.control,
    borderWidth: border.rest,
    borderColor: colors.line,
  },
  // Selected is the 3px ink border; it takes its width out of the padding.
  chosen: { borderWidth: border.selected, borderColor: colors.ink, paddingHorizontal: space.md },
  name: { ...type.body, color: colors.ink, flexShrink: 1 },
  kind: { ...type.caption, color: colors.muted, marginLeft: space.sm },
  none: { ...type.caption, color: colors.muted, marginTop: space.sm },
});

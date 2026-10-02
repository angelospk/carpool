import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';

import { api } from '../api/endpoints';
import { colors, radius, space, type } from '../design/tokens';

/**
 * This month of a group, in numbers, for its admin.
 *
 * The same trips the export lists — both sides confirmed, both members on the
 * day — counted rather than named. The export is where the names are; this is
 * what an admin says out loud in a meeting.
 */
export function GroupStats({ groupId }: { groupId: string }) {
  const { t } = useTranslation();
  const stats = useQuery({ queryKey: ['groups', groupId, 'stats'], queryFn: () => api.groupStats(groupId) });
  if (!stats.data) return null;

  const cells = [
    { key: 'trips', value: stats.data.trips },
    { key: 'seats', value: stats.data.seats },
    { key: 'drivers', value: stats.data.drivers },
    { key: 'members', value: stats.data.members },
  ] as const;

  return (
    <View style={styles.wrap}>
      <Text style={styles.label}>{t('groups.statsTitle')}</Text>
      <View style={styles.grid}>
        {cells.map((c) => (
          <View key={c.key} style={styles.cell}>
            <Text testID={`group-stats-${c.key}`} style={styles.value}>
              {c.value}
            </Text>
            <Text style={styles.caption}>{t(`groups.stats_${c.key}`)}</Text>
          </View>
        ))}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: space.sm, marginTop: space.sm },
  label: { ...type.label, color: colors.muted },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: space.sm },
  cell: {
    flexGrow: 1,
    flexBasis: '45%',
    backgroundColor: colors.sunken,
    borderRadius: radius.inner,
    padding: space.md,
  },
  value: { ...type.display, color: colors.ink },
  caption: { ...type.caption, color: colors.muted },
});

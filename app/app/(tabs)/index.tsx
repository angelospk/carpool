import { FlatList, Pressable, StyleSheet, Text, View } from 'react-native';
import { useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';

import { api, type TripSummary } from '../../src/api/endpoints';
import { AsyncState, Screen, colors } from '../../src/components/ui';

export default function HomeScreen() {
  const { t } = useTranslation();
  const router = useRouter();

  const trips = useQuery({
    queryKey: ['trips', 'upcoming'],
    queryFn: () => api.upcomingTrips(),
  });

  return (
    <Screen>
      <AsyncState
        loading={trips.isLoading}
        error={trips.error}
        onRetry={() => void trips.refetch()}
        empty={!trips.data?.trips.length}
        emptyLabel={t('home.empty')}
      >
        <FlatList
          testID="trip-list"
          data={trips.data?.trips ?? []}
          keyExtractor={(item) => item.id}
          refreshing={trips.isRefetching}
          onRefresh={() => void trips.refetch()}
          renderItem={({ item }) => (
            <TripRow trip={item} onPress={() => router.push(`/trip/${item.id}`)} />
          )}
        />
      </AsyncState>
    </Screen>
  );
}

function TripRow({ trip, onPress }: { trip: TripSummary; onPress: () => void }) {
  const { t } = useTranslation();
  // Times come from the server as instants; the device renders them in Athens
  // so a traveller does not see a trip an hour out.
  const when = new Date(trip.depart_earliest).toLocaleString('el-GR', {
    timeZone: 'Europe/Athens',
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  });

  return (
    <Pressable testID={`trip-${trip.id}`} onPress={onPress} style={styles.row}>
      <View style={styles.rowTop}>
        <Text style={styles.route}>
          {trip.origin_label} → {trip.destination_label}
        </Text>
        <Text style={styles.badge}>
          {trip.role === 'driver' ? t('home.asDriver') : t('home.asPassenger')}
        </Text>
      </View>
      <Text style={styles.when}>{when}</Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  row: { paddingVertical: 16, borderBottomWidth: 1, borderBottomColor: colors.border, gap: 4 },
  rowTop: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  route: { fontSize: 16, fontWeight: '600', color: colors.text, flexShrink: 1 },
  badge: { fontSize: 12, color: colors.accent, fontWeight: '600' },
  when: { fontSize: 14, color: colors.muted },
});

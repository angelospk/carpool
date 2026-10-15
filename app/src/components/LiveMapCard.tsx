// What is on the road now, for somebody deciding whether to ask for a seat.
//
// Anonymous by construction: the server sends each car as two settlement names,
// a line that stops a kilometre short of both ends, and its free seats. No
// driver, no live position. See store.LiveMap.

import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';

import { api } from '../api/endpoints';
import { Map } from '../map/Map';
import { Card } from './ui';
import { colors, space, type } from '../design/tokens';

// The middle of Evros, at a zoom that shows it from Αλεξανδρούπολη to Ορμένιο.
const EVROS = { lon: 26.15, lat: 41.15 };
const ROWS = 5;

export function LiveMapCard() {
  const { t } = useTranslation();
  const live = useQuery({
    queryKey: ['live-map'],
    queryFn: () => api.liveMap(),
    // A car on the road is there for half an hour or so; a minute is fresh.
    refetchInterval: 60_000,
  });

  // Nothing at all rather than an error: this card is context, not the task.
  // Nor when the road is empty: it sits above the search form, and a card
  // saying there is nothing only pushes the form down.
  if (!live.isSuccess || live.data.trips.length === 0) return null;
  const trips = live.data.trips;

  return (
    <Card testID="live-map">
      <Text style={styles.title}>{t('onRoad.title')}</Text>
      <Map testID="live-map-map" centre={EVROS} zoom={8} lines={trips.map((trip) => trip.polyline)} />
      <View style={styles.rows}>
        {trips.slice(0, ROWS).map((trip, i) => (
          <Text key={i} style={styles.row}>
            {`${trip.from} → ${trip.to} · ${t('onRoad.seats', { count: trip.seats_free })}`}
          </Text>
        ))}
        {trips.length > ROWS ? (
          <Text style={styles.more}>{t('onRoad.more', { count: trips.length - ROWS })}</Text>
        ) : null}
      </View>
    </Card>
  );
}

const styles = StyleSheet.create({
  title: { ...type.label, color: colors.ink, marginBottom: space.xs },
  rows: { gap: space.xs, marginTop: space.sm },
  row: { ...type.body, color: colors.ink },
  more: { ...type.caption, color: colors.muted },
});

// Which car this trip goes out in.
//
// It used to hide itself whenever there was nothing to choose, which meant a
// driver with one car never saw which car they were publishing with — and a
// driver with none saw nothing at all until the server refused the whole form.
// Both are answers worth showing: the trip goes out in a particular car, the
// passenger is told its colour and plate, and an empty garage is the reason
// publishing is about to fail.
//
// So the section is always there, and says which of the four things is true:
// still loading, the garage did not load, there is no car yet, or here is the
// car — pick another if you have one.

import { useEffect } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { useRouter } from 'expo-router';

import { api, type Vehicle } from '../api/endpoints';
import { colors, radius, space, type } from '../design/tokens';
import { Chip } from './ui';

export function VehiclePicker({
  value,
  onChange,
  testID,
}: {
  /** The chosen car, or null for "whichever is the default". */
  value: string | null;
  onChange: (id: string | null) => void;
  testID?: string;
}) {
  const { t } = useTranslation();
  const router = useRouter();
  const { data: vehicles, isLoading, error, refetch } = useQuery({
    queryKey: ['vehicles'],
    queryFn: api.vehicles,
  });

  // What the server would pick if the form said nothing. Computed before the
  // early returns below, because the effect that reconciles it has to run on
  // every render whether or not there is a garage to show.
  const fallback = vehicles?.length ? (vehicles.find((v) => v.is_default) ?? vehicles[0]).id : null;
  const stale = value !== null && !!vehicles && !vehicles.some((v) => v.id === value);

  // A car retired from another screen leaves this form holding an id the server
  // will refuse. Showing the fallback and sending the dead id is the worst of
  // both: the driver reads the right car off the screen and is told to add one.
  useEffect(() => {
    if (stale) onChange(fallback);
  }, [stale, fallback, onChange]);

  if (isLoading) {
    return (
      <View testID={testID} style={styles.box}>
        <Text style={styles.hint}>{t('common.loading')}</Text>
      </View>
    );
  }

  if (error) {
    return (
      <View testID={testID} style={styles.box}>
        <Pressable testID={testID ? `${testID}-retry` : undefined} onPress={() => void refetch()}>
          <Text style={styles.hint}>{t('publish.carFailed')}</Text>
          <Text style={styles.link}>{t('common.retry')}</Text>
        </Pressable>
      </View>
    );
  }

  const garage = vehicles ?? [];

  if (garage.length === 0) {
    return (
      <View testID={testID} style={styles.box}>
        <Text style={styles.hint}>{t('publish.noCar')}</Text>
        <Pressable
          testID={testID ? `${testID}-add` : undefined}
          accessibilityRole="button"
          onPress={() => router.push('/profile/vehicles')}
        >
          <Text style={styles.link}>{t('publish.addCar')}</Text>
        </Pressable>
      </View>
    );
  }

  // Shown as chosen from the start rather than leaving the row looking
  // unanswered. The effect above has already told the form the same thing, so
  // what is drawn here and what gets published are the one car.
  const chosen = garage.some((v) => v.id === value) ? (value as string) : (fallback as string);
  const car = garage.find((v) => v.id === chosen) as Vehicle;

  return (
    <View testID={testID} style={styles.box}>
      <Text style={styles.label}>{t('publish.whichCar')}</Text>

      {garage.length === 1 ? (
        // One car is a fact, not a question. It is stated, with the way out to
        // the garage for the driver who has just bought a second.
        <View style={styles.single}>
          <Text testID={testID ? `${testID}-only` : undefined} style={styles.carName}>
            {car.name}
          </Text>
          <Pressable
            testID={testID ? `${testID}-change` : undefined}
            accessibilityRole="button"
            onPress={() => router.push('/profile/vehicles')}
          >
            <Text style={styles.link}>{t('publish.changeCar')}</Text>
          </Pressable>
        </View>
      ) : (
        <View style={styles.row}>
          {garage.map((v) => (
            <Chip
              key={v.id}
              testID={testID ? `${testID}-${v.id}` : undefined}
              label={v.name}
              selected={v.id === chosen}
              onPress={() => onChange(v.id)}
            />
          ))}
        </View>
      )}

      {/* The colour and the plate are what a passenger looks for at the kerb,
          so the driver sees the same two things they are about to promise. */}
      <Text testID={testID ? `${testID}-detail` : undefined} style={styles.hint}>
        {[car.colour, car.plate].filter(Boolean).join(' · ')}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  box: { gap: space.xs },
  label: { ...type.body, color: colors.ink },
  row: { flexDirection: 'row', flexWrap: 'wrap', gap: space.xs, borderRadius: radius.pill },
  single: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  carName: { ...type.label, color: colors.ink },
  hint: { ...type.caption, color: colors.muted },
  link: { ...type.caption, color: colors.primary },
});

// Which car this trip goes out in.
//
// It hides itself when there is nothing to decide. A driver with one car — which
// is most of them — should never see a question whose only answer is the one
// they already gave, and a driver with none has a different problem that the
// publish screen states plainly.
//
// Choosing nothing is a real answer: the server publishes with the default, so
// the default is shown as selected from the start rather than leaving the row
// looking unanswered.

import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';

import { api } from '../api/endpoints';
import { colors, radius, space, type } from '../design/tokens';
import { Chip } from './ui';

export function VehiclePicker({
  value,
  onChange,
  testID,
}: {
  /** The chosen car, or null for "whichever is the default". */
  value: string | null;
  onChange: (id: string) => void;
  testID?: string;
}) {
  const { t } = useTranslation();
  const { data: vehicles } = useQuery({ queryKey: ['vehicles'], queryFn: api.vehicles });

  if (!vehicles || vehicles.length < 2) return null;

  const chosen = value ?? vehicles.find((v) => v.is_default)?.id ?? vehicles[0].id;

  return (
    <View testID={testID} style={styles.box}>
      <Text style={styles.label}>{t('publish.whichCar')}</Text>
      <View style={styles.row}>
        {vehicles.map((v) => (
          <Chip
            key={v.id}
            testID={testID ? `${testID}-${v.id}` : undefined}
            label={v.name}
            selected={v.id === chosen}
            onPress={() => onChange(v.id)}
          />
        ))}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  box: { gap: space.xs },
  label: { ...type.body, color: colors.ink },
  row: { flexDirection: 'row', flexWrap: 'wrap', gap: space.xs, borderRadius: radius.pill },
});

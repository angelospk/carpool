// Why this seat costs what it costs.
//
// "3,50 €" answers nothing. The question a passenger actually has is whether
// that is a fare, and the answer is the division: the whole car's fuel over
// every seat the driver offered plus the driver, floored to the half euro, plus
// the barriers they will be in the car for. Showing the division is showing
// that nobody is making anything.
//
// Every number here comes from the server, off the receipt frozen when the seat
// was booked. Nothing is recomputed on the phone — a client doing its own
// arithmetic would disagree with the price beside it the day a fuel figure
// moved, and would disagree differently on every handset.

import { useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';

import { type PriceBreakdownData } from '../api/endpoints';
import { colors, space, type } from '../design/tokens';

const euro = (cents: number) => `${(cents / 100).toFixed(2).replace('.', ',')} €`;

export function PriceBreakdown({
  breakdown,
  testID,
}: {
  breakdown: PriceBreakdownData;
  testID?: string;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const b = breakdown;

  if (b.waived) {
    return (
      <Text testID={testID ? `${testID}-waived` : undefined} style={styles.note}>
        {t('price.waived')}
      </Text>
    );
  }
  if (b.mode !== 'formula') {
    return (
      <Text testID={testID ? `${testID}-mode` : undefined} style={styles.note}>
        {t(`price.mode.${b.mode}`)}
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
        {t('price.explain')}
      </Text>
    );
  }

  return (
    <View testID={testID} style={styles.box}>
      <Line
        label={t('price.fuelForTheTrip', {
          litres: b.consumption_l_100km.toFixed(1),
          price: b.fuel_price_eur.toFixed(3).replace('.', ','),
          km: b.trip_km.toFixed(0),
        })}
        value={euro(b.fuel_cost_cents)}
        testID={testID ? `${testID}-fuel` : undefined}
      />
      <Line
        label={t('price.dividedBy', { divisor: b.divisor })}
        value={euro(b.share_cents)}
        testID={testID ? `${testID}-divided` : undefined}
      />
      {b.share_cents !== b.rounded_cents ? (
        <Line
          label={t('price.roundedDown')}
          value={euro(b.rounded_cents)}
          testID={testID ? `${testID}-rounded` : undefined}
        />
      ) : null}
      {b.toll_share_cents > 0 ? (
        <Line
          label={t('price.tolls')}
          value={euro(b.toll_share_cents)}
          testID={testID ? `${testID}-tolls` : undefined}
        />
      ) : null}

      <View style={styles.rule} />
      <Line
        label={t('price.total')}
        value={euro(b.total_cents)}
        strong
        testID={testID ? `${testID}-total` : undefined}
      />

      <Text style={styles.note}>{t('price.notAFare')}</Text>
      {b.consumption_source === 'band' ? (
        <Text style={styles.note}>{t('price.consumptionIsAFloor')}</Text>
      ) : null}
      {b.region_name && !b.price_is_regional ? (
        <Text style={styles.note}>{t('price.nationalFuelPrice')}</Text>
      ) : null}
    </View>
  );
}

function Line({
  label,
  value,
  strong,
  testID,
}: {
  label: string;
  value: string;
  strong?: boolean;
  testID?: string;
}) {
  return (
    <View testID={testID} style={styles.line}>
      <Text style={[styles.label, strong && styles.strong]}>{label}</Text>
      <Text style={[styles.value, strong && styles.strong]}>{value}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  box: { gap: space.xs, marginTop: space.sm },
  line: { flexDirection: 'row', justifyContent: 'space-between', gap: space.sm },
  label: { ...type.caption, color: colors.muted, flex: 1 },
  value: { ...type.caption, color: colors.muted },
  strong: { color: colors.ink },
  rule: { height: 1, backgroundColor: colors.line, marginVertical: space.xs },
  link: { ...type.caption, color: colors.primary, marginTop: space.sm },
  note: { ...type.caption, color: colors.muted, marginTop: space.xs },
});

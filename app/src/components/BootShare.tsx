// The boot, minus what the driver is bringing.
//
// `luggage_capacity` is compared by the matcher against the luggage passengers
// have already booked and against nothing else — the driver's own bags are not
// modelled anywhere in the schema. So the number has always meant "places left
// for a passenger", while the form asked "πόσες χωράνε", which is the whole
// boot. A driver bringing a suitcase of their own over-promised by one, and
// found out when somebody turned up with a bag and no room.
//
// The correction is to the question rather than to the schema. The picker above
// is labelled as the places that remain; this is a calculator for a driver who
// would rather think in "boot, minus mine" and have the answer filled in.
//
// Neither number here is sent or stored. That is deliberate: if the driver's
// own luggage were folded into the stored capacity, reopening the route would
// show the remainder as the total and a second glance would subtract it again.
// The stored figure keeps one meaning, and this is scaffolding around it.

import { useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';

import { Chip } from './ui';
import { colors, space, type } from '../design/tokens';

/** Boots this app will believe in. The upper bound is the CHECK on
    route_templates.luggage_capacity: eight places is a van with the seats
    down. */
const TOTALS = [1, 2, 3, 4, 5, 6, 7, 8] as const;

export function BootShare({
  onResult,
  testID,
}: {
  /** Called with the places that remain as soon as both halves are known —
      including zero, which the form cannot send but must not hide. */
  onResult: (available: number) => void;
  testID?: string;
}) {
  const { t } = useTranslation();
  const [total, setTotal] = useState<number | null>(null);
  const [own, setOwn] = useState<number | null>(null);

  // Own luggage is offered up to the whole boot, not up to one less. A driver
  // filling their own car is telling the truth, and being quietly clamped to
  // leave one place they never claimed is how a form invents data.
  const owns = total === null ? [] : Array.from({ length: total + 1 }, (_, i) => i);
  const left = total !== null && own !== null ? total - own : null;

  const settle = (nextTotal: number | null, nextOwn: number | null) => {
    setTotal(nextTotal);
    setOwn(nextOwn);
    if (nextTotal === null || nextOwn === null) return;
    // Zero is a real answer and not one this field can carry: the CHECK pairs a
    // capacity of zero with the size 'none'. It is still reported, because the
    // alternative was worse — swallowing it left the previous, larger number in
    // the form, so a driver who said "the boot is full" published a promise of
    // three places. The publish screen refuses the pair instead.
    onResult(nextTotal - nextOwn);
  };

  return (
    <View testID={testID} style={styles.box}>
      <Text style={styles.label}>{t('luggage.bootTotal')}</Text>
      <View style={styles.row}>
        {TOTALS.map((n) => (
          <Chip
            key={n}
            testID={`boot-total-${n}`}
            label={String(n)}
            selected={total === n}
            // Choosing a smaller boot than the driver already claimed for
            // themselves would leave a negative remainder on screen.
            onPress={() => settle(n, own === null ? null : Math.min(own, n))}
          />
        ))}
      </View>

      {total !== null ? (
        <>
          <Text style={styles.label}>{t('luggage.bootOwn')}</Text>
          <View style={styles.row}>
            {owns.map((n) => (
              <Chip
                key={n}
                testID={`boot-own-${n}`}
                label={String(n)}
                selected={own === n}
                onPress={() => settle(total, n)}
              />
            ))}
          </View>
        </>
      ) : null}

      {left !== null && left > 0 ? (
        <Text testID="boot-left" style={styles.result}>
          {t('luggage.bootLeft', { count: left })}
        </Text>
      ) : null}

      {left === 0 ? (
        <Text testID="boot-nothing-left" style={styles.warning}>
          {t('luggage.bootNothingLeft')}
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  box: { gap: space.xs },
  label: { ...type.caption, color: colors.ink },
  row: { flexDirection: 'row', flexWrap: 'wrap', gap: space.xs },
  result: { ...type.label, color: colors.ink },
  warning: { ...type.caption, color: colors.danger },
});

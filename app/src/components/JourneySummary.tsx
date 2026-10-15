import { StyleSheet, Text, View } from 'react-native';

import { RouteSpine } from './ui';
import { athensDay, athensTime, startsNewDay } from '../design/format';
import { colors, space, type } from '../design/tokens';

/**
 * One journey, the same way everywhere: when, the two places, and one quiet
 * line saying where it stands.
 *
 * The home card, the request card and the request screen each used to draw the
 * same journey their own way — an arrow string here, a spine there — so going
 * from wanting a ride to waiting for one to having one read as three different
 * things. This is presentation only: whoever uses it decides what the status
 * says and what tone it takes.
 */
export function JourneySummary({
  from,
  to,
  start,
  end,
  status,
  statusTone = 'accent',
  testID,
  routeTestID,
  statusTestID,
}: {
  from: string;
  to: string;
  start?: string;
  end?: string;
  status?: string;
  statusTone?: 'accent' | 'muted' | 'danger';
  testID?: string;
  routeTestID?: string;
  statusTestID?: string;
}) {
  const when = whenLine(start, end);
  return (
    <View testID={testID} style={styles.wrap}>
      {when ? (
        <Text testID={testID ? `${testID}-when` : undefined} style={styles.when}>
          {when}
        </Text>
      ) : null}
      <View testID={routeTestID}>
        <RouteSpine from={from} to={to} />
      </View>
      {status ? (
        <Text
          testID={statusTestID ?? (testID ? `${testID}-status` : undefined)}
          style={[
            styles.status,
            statusTone === 'muted' && styles.statusMuted,
            statusTone === 'danger' && styles.statusDanger,
          ]}
        >
          {status}
        </Text>
      ) : null}
    </View>
  );
}

/**
 * "Τρ 29 Σεπ · 07:00–08:00", in Athens.
 *
 * A window that crosses midnight names both days, because one day followed by
 * two times says both are on that day. A window that is a moment is one time.
 */
export function whenLine(start?: string, end?: string): string {
  if (!start) return '';
  const head = `${athensDay(start)} · ${athensTime(start)}`;
  if (!end || new Date(end).getTime() === new Date(start).getTime()) return head;
  if (startsNewDay(end, start)) return `${head} – ${athensDay(end)} · ${athensTime(end)}`;
  return `${head}–${athensTime(end)}`;
}

const styles = StyleSheet.create({
  wrap: { gap: space.sm },
  when: { ...type.label, color: colors.ink },
  status: { ...type.label, fontSize: 13, color: colors.ink },
  statusMuted: { color: colors.muted },
  statusDanger: { color: colors.danger },
});

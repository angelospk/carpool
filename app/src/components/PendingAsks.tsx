import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { useRouter } from 'expo-router';

import { api } from '../api/endpoints';
import { useSession } from '../state/session';
import { athensTime } from '../design/format';
import { detourText } from '../design/detour';
import { Badge, Card } from './ui';
import { JourneySummary } from './JourneySummary';
import { colors, space, type } from '../design/tokens';

/**
 * The questions waiting on this driver, at the top of the home screen.
 *
 * The push used to be the only way to hear of one, and it opened the front
 * page, where nothing mentioned it. Polled for the same reason the requests
 * card is: the question arrives from somebody else's phone.
 *
 * Amber, because the palette keeps amber for anything that wants an answer.
 */
export function PendingAsks() {
  const { t } = useTranslation();
  const router = useRouter();
  const signedIn = useSession((s) => s.status === 'signedIn');
  const asks = useQuery({
    queryKey: ['asks', 'pending'],
    queryFn: () => api.pendingAsks(),
    enabled: signedIn,
    refetchInterval: 30_000,
  });

  const list = asks.data?.asks ?? [];
  if (list.length === 0) return null;

  return (
    <View testID="pending-asks" style={styles.wrap}>
      <Text style={styles.heading}>{t('ask.forYou')}</Text>
      {list.map((a) => (
        <Card
          key={a.id}
          tone="accent"
          testID={`pending-ask-${a.id}`}
          onPress={() => router.push(`/ask/${a.id}`)}
        >
          <View style={styles.top}>
            <Text style={styles.who} numberOfLines={1}>
              {t('ask.who', { name: a.passenger_name, count: a.seats })}
            </Text>
            <Badge label={t('ask.open')} tone="accent" />
          </View>
          <JourneySummary
            from={a.origin_label}
            to={a.destination_label}
            start={a.depart_earliest}
            status={`${detourText(a.detour_min, a.detour_km, t)} · ${t('ask.answerBy', {
              time: athensTime(a.expires_at),
            })}`}
          />
        </Card>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: space.sm },
  heading: { ...type.caption, color: colors.muted, marginLeft: space.xs },
  top: { flexDirection: 'row', alignItems: 'center', gap: space.sm, marginBottom: space.sm },
  who: { ...type.body, color: colors.ink, flex: 1, fontWeight: '700' },
});

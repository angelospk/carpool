import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { useRouter } from 'expo-router';
import type { TFunction } from 'i18next';

import { api, type PendingAsk } from '../api/endpoints';
import { useSession } from '../state/session';
import { athensTime, decimal, eur } from '../design/format';
import { detourText } from '../design/detour';
import { Avatar, Badge, Card } from './ui';
import { JourneySummary } from './JourneySummary';
import { SharedGroups } from './SharedGroups';
import { colors, space, type } from '../design/tokens';

/** The price is per seat; with more than one seat the total is said too. */
export function askPriceText(a: Pick<PendingAsk, 'price_eur' | 'seats'>, t: TFunction): string {
  if (a.seats > 1) {
    return t('ask.pricePerSeat', { price: eur(a.price_eur), total: eur(a.price_eur * a.seats) });
  }
  return eur(a.price_eur);
}

/** What the driver decides on about the passenger: face, stars, shared groups,
    when they can go and what they carry. */
export function AskPassenger({ ask: a, showLuggageNone = false }: { ask: PendingAsk; showLuggageNone?: boolean }) {
  const { t } = useTranslation();
  const hasLuggage = a.luggage !== 'none' && a.luggage_count > 0;
  return (
    <View style={styles.passenger}>
      <Avatar name={a.passenger_name} photoUrl={a.passenger_photo_url} size={40} />
      <View style={styles.passengerText}>
        <Text testID="ask-rating" style={styles.meta}>
          {a.passenger_rating != null
            ? `★ ${decimal(a.passenger_rating, 1)} (${a.passenger_rating_count})`
            : t('ask.unrated')}
        </Text>
        <SharedGroups testID="ask-shared-groups" names={a.shared_groups} />
        <Text testID="ask-window" style={styles.meta}>
          {t('ask.window', { from: athensTime(a.window_start), to: athensTime(a.window_end) })}
        </Text>
        {hasLuggage || showLuggageNone ? (
          <Text testID="ask-luggage" style={styles.meta}>
            {hasLuggage
              ? t('ask.luggage', { count: a.luggage_count, size: t(`luggage.${a.luggage}`) })
              : t('luggage.none')}
          </Text>
        ) : null}
      </View>
    </View>
  );
}

/**
 * The questions waiting on this driver, at the top of the home screen.
 *
 * The push used to be the only way to hear of one, and it opened the front
 * page, where nothing mentioned it. Polled for the same reason the requests
 * card is: the question arrives from somebody else's phone.
 *
 * Amber, because the palette keeps amber for anything that wants an answer.
 */
export function PendingAsks({ occurrenceId }: { occurrenceId?: string } = {}) {
  const { t } = useTranslation();
  const router = useRouter();
  const signedIn = useSession((s) => s.status === 'signedIn');
  const asks = useQuery({
    queryKey: ['asks', 'pending'],
    queryFn: () => api.pendingAsks(),
    enabled: signedIn,
    refetchInterval: 30_000,
  });

  const list = (asks.data?.asks ?? []).filter((a) => !occurrenceId || a.occurrence_id === occurrenceId);
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
          <AskPassenger ask={a} />
          {a.price_eur > 0 ? (
            <Text testID={`pending-ask-price-${a.id}`} style={styles.price}>
              {askPriceText(a, t)}
            </Text>
          ) : null}
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
  who: { ...type.heading, color: colors.ink, flex: 1 },
  passenger: { flexDirection: 'row', gap: space.sm, marginBottom: space.sm },
  passengerText: { flex: 1, gap: space.xs },
  meta: { ...type.caption, color: colors.muted },
  price: { ...type.body, color: colors.ink, marginBottom: space.sm },
});

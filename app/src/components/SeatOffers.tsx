// Seats drivers offered this passenger, on the home screen.
//
// A weekend request is not offered to drivers one by one: the driver says yes
// first, and only then does the passenger hear of it. So this is the one place
// an answer is wanted from a passenger, and it lapses — amber, like the asks a
// driver is waiting on.

import { useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useRouter } from 'expo-router';

import { api, type DriverOffer } from '../api/endpoints';
import { ApiError } from '../api/client';
import { refusalText } from '../api/errorText';
import { useSession } from '../state/session';
import { athensTime, decimal, seatPrice } from '../design/format';
import { Avatar, Button, Card } from './ui';
import { JourneySummary } from './JourneySummary';
import { colors, space, type } from '../design/tokens';

/**
 * Why an offer cannot be answered, in words. These are the same refusals an ask
 * gets, and the offer is the same ask seen from the other side.
 */
function offerError(e: unknown, t: TFunction): string {
  if (e instanceof ApiError) {
    switch (e.code) {
      case 'ask_expired':
        return t('ask.expired');
      case 'no_seats':
        return t('ask.noSeats');
      case 'ask_closed':
        return t('ask.closed');
      case 'party_cap':
        return t('ask.partyCap');
      case 'itinerary_changed':
        return t('ask.noLongerFits');
      case 'no_luggage_room':
        return t('ask.noLuggageRoom');
      case 'not_found':
        return t('ask.gone');
    }
  }
  return refusalText(e) ?? t('common.error');
}

export function SeatOffers() {
  const { t } = useTranslation();
  const signedIn = useSession((s) => s.status === 'signedIn');
  // Polled: the offer arrives from somebody else's phone.
  const offers = useQuery({
    queryKey: ['offers'],
    queryFn: () => api.offers(),
    enabled: signedIn,
    refetchInterval: 30_000,
  });

  const list = offers.data?.offers ?? [];
  if (list.length === 0) return null;

  return (
    <View testID="seat-offers" style={styles.wrap}>
      <Text style={styles.heading}>{t('weekend.offersTitle')}</Text>
      {list.map((o) => (
        <OfferCard key={o.id} offer={o} />
      ))}
    </View>
  );
}

function OfferCard({ offer: o }: { offer: DriverOffer }) {
  const { t } = useTranslation();
  const router = useRouter();
  const queryClient = useQueryClient();
  const [outcome, setOutcome] = useState<string | null>(null);

  const answered = () => {
    void queryClient.invalidateQueries({ queryKey: ['offers'] });
    void queryClient.invalidateQueries({ queryKey: ['requests'] });
  };
  const accept = useMutation({
    mutationFn: () => api.acceptOffer(o.id),
    onSuccess: () => {
      // The seat is a trip now, and the request is no longer being looked for.
      void queryClient.invalidateQueries({ queryKey: ['trips'] });
      answered();
      router.push(`/trip/${o.occurrence_id}`);
    },
    onError: (e) => {
      setOutcome(offerError(e, t));
      // Whatever closed it, it is no longer an offer worth showing.
      if (e instanceof ApiError && e.status >= 400 && e.status < 500) answered();
    },
  });
  const decline = useMutation({
    mutationFn: () => api.declineOffer(o.id),
    onSuccess: answered,
    onError: (e) => setOutcome(offerError(e, t)),
  });

  const car = [o.car.make, o.car.model, o.car.colour].filter(Boolean).join(' · ');
  const busy = accept.isPending || decline.isPending;

  return (
    <Card tone="accent" testID={`seat-offer-${o.id}`}>
      <View style={styles.driver}>
        <Avatar name={o.driver_name} photoUrl={o.driver_photo_url} size={40} />
        <View style={styles.driverText}>
          <Text style={styles.who} numberOfLines={1}>
            {o.driver_name}
          </Text>
          <Text testID={`seat-offer-rating-${o.id}`} style={styles.meta}>
            {o.driver_rating != null
              ? `★ ${decimal(o.driver_rating, 1)} (${o.driver_rating_count})`
              : t('ask.unrated')}
          </Text>
          {car ? <Text style={styles.meta}>{car}</Text> : null}
        </View>
      </View>
      {o.price_eur > 0 ? (
        <Text testID={`seat-offer-price-${o.id}`} style={styles.price}>
          {seatPrice(t, o.price_eur, o.seats)}
        </Text>
      ) : null}
      <JourneySummary
        from={o.origin_label}
        to={o.destination_label}
        start={o.depart_earliest}
        status={t('weekend.offerExpires', { time: athensTime(o.expires_at) })}
      />
      {outcome ? <Text style={styles.meta}>{outcome}</Text> : null}
      <View style={styles.answers}>
        <View style={styles.answer}>
          <Button
            testID={`seat-offer-accept-${o.id}`}
            label={t('weekend.accept')}
            disabled={busy}
            onPress={() => {
              setOutcome(null);
              accept.mutate();
            }}
          />
        </View>
        <View style={styles.answer}>
          <Button
            testID={`seat-offer-decline-${o.id}`}
            label={t('weekend.decline')}
            variant="secondary"
            disabled={busy}
            onPress={() => {
              setOutcome(null);
              decline.mutate();
            }}
          />
        </View>
      </View>
    </Card>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: space.sm },
  heading: { ...type.caption, color: colors.muted, marginLeft: space.xs },
  driver: { flexDirection: 'row', gap: space.sm, marginBottom: space.sm },
  driverText: { flex: 1, gap: 2 },
  who: { ...type.heading, color: colors.ink },
  meta: { ...type.caption, color: colors.muted },
  price: { ...type.body, color: colors.ink, marginBottom: space.sm },
  answers: { flexDirection: 'row', gap: space.sm, marginTop: space.sm },
  answer: { flex: 1 },
});

// The passengers waiting on a driver's road for a weekend, and a «Ναι» for each.
//
// The driver decides first: nobody is asked a question the passenger has not
// seen the answer to. So after publishing, and on the trip itself, this lists
// the open weekend requests a departure could carry. «Ναι» is an offer, not a
// booking: the passenger answers it.

import { useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery } from '@tanstack/react-query';

import { api, type WaitingPassenger } from '../api/endpoints';
import { ApiError } from '../api/client';
import { refusalText } from '../api/errorText';
import { useSession } from '../state/session';
import { athensTime } from '../design/format';
import { athensIsoWeekday, isWeekendDay } from '../design/weekend';
import { Button } from './ui';
import { colors, space, type } from '../design/tokens';

export function WaitingPassengers({
  occurrenceId,
  departs,
}: {
  occurrenceId: string;
  /** When the departure leaves, which names the day in the question. */
  departs: string;
}) {
  const { t } = useTranslation();
  const signedIn = useSession((s) => s.status === 'signedIn');
  const waiting = useQuery({
    queryKey: ['waiting', occurrenceId],
    queryFn: () => api.waiting(occurrenceId),
    enabled: signedIn,
  });
  // Who has been offered, and who the offer failed for. Kept here rather than
  // read back from the list: the list does not say who was offered.
  const [offered, setOffered] = useState<Set<string>>(() => new Set());
  const [refused, setRefused] = useState<Record<string, string>>({});

  const offer = useMutation({
    mutationFn: (requestId: string) => api.offerSeat(occurrenceId, requestId),
    onSuccess: (_, requestId) => setOffered((s) => new Set(s).add(requestId)),
    onError: (e, requestId) => {
      // Already offered is the state the driver wanted.
      if (e instanceof ApiError && e.code === 'already_offered') {
        return setOffered((s) => new Set(s).add(requestId));
      }
      setRefused((r) => ({ ...r, [requestId]: refusalText(e) ?? t('common.error') }));
    },
  });

  // Nobody waiting, or a trip that cannot be asked about, is not worth a word.
  const list = waiting.data?.waiting ?? [];
  if (list.length === 0) return null;

  const day = t(`weekend.onDay.${athensIsoWeekday(new Date(departs))}`);

  return (
    <View testID={`waiting-${occurrenceId}`} style={styles.wrap}>
      <Text testID={`waiting-${occurrenceId}-title`} style={styles.title}>
        {t('weekend.waiting', { count: list.length, day })}
      </Text>
      {list.map((p) => (
        <Row
          key={p.request_id}
          person={p}
          offered={offered.has(p.request_id)}
          pending={offer.isPending && offer.variables === p.request_id}
          refusal={refused[p.request_id]}
          onYes={() => {
            setRefused(({ [p.request_id]: _gone, ...rest }) => rest);
            offer.mutate(p.request_id);
          }}
        />
      ))}
    </View>
  );
}

function Row({
  person: p,
  offered,
  pending,
  refusal,
  onYes,
}: {
  person: WaitingPassenger;
  offered: boolean;
  pending: boolean;
  refusal?: string;
  onYes: () => void;
}) {
  const { t } = useTranslation();
  const hasLuggage = p.luggage !== 'none' && p.luggage_count > 0;
  return (
    <View testID={`waiting-row-${p.request_id}`} style={styles.row}>
      <View style={styles.rowText}>
        <Text style={styles.name} numberOfLines={1}>
          {p.passenger_name}
        </Text>
        <Text style={styles.meta}>
          {[
            t('weekend.seats', { count: p.seats }),
            hasLuggage ? t('ask.luggage', { count: p.luggage_count, size: t(`luggage.${p.luggage}`) }) : null,
            t('ask.window', { from: athensTime(p.window_start), to: athensTime(p.window_end) }),
          ]
            .filter(Boolean)
            .join(' · ')}
        </Text>
        {refusal ? <Text style={styles.meta}>{refusal}</Text> : null}
      </View>
      {offered ? (
        <Text testID={`waiting-offered-${p.request_id}`} style={styles.offered}>
          {t('weekend.offered')}
        </Text>
      ) : (
        <View style={styles.yes}>
          <Button
            testID={`waiting-yes-${p.request_id}`}
            label={t('weekend.yes')}
            disabled={pending}
            onPress={onYes}
          />
        </View>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: space.sm },
  title: { ...type.label, color: colors.ink },
  row: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
  rowText: { flex: 1, gap: 2 },
  name: { ...type.body, color: colors.ink },
  meta: { ...type.caption, color: colors.muted },
  yes: { width: 96 },
  offered: { ...type.label, color: colors.ink, maxWidth: 140, textAlign: 'right' },
});

/**
 * The weekend departures of a route just published, each with its own waiting
 * list. The publish answer names no departures except a trip leaving now, so
 * they are found among the driver's upcoming trips by the route's template:
 * place labels are not enough, another route can share both ends.
 */
export function WeekendDepartures({ templateId }: { templateId: string }) {
  const signedIn = useSession((s) => s.status === 'signedIn');
  // Always asked again on mount: the route that was just published is not in
  // a list fetched a minute ago.
  const trips = useQuery({
    queryKey: ['trips', 'upcoming'],
    queryFn: () => api.upcomingTrips(),
    enabled: signedIn,
    refetchOnMount: 'always',
  });

  const now = Date.now();
  const departures = (trips.data?.trips ?? [])
    .filter(
      (x) =>
        x.role === 'driver' &&
        x.template_id === templateId &&
        Date.parse(x.depart_earliest) > now &&
        isWeekendDay(new Date(x.depart_earliest)),
    )
    .sort((a, b) => Date.parse(a.depart_earliest) - Date.parse(b.depart_earliest))
    // A route is made weeks ahead; the nearest weekends are the ones people
    // are waiting for, and each departure is a request to the server.
    .slice(0, MAX_DEPARTURES);

  if (departures.length === 0) return null;
  return (
    <View testID="weekend-departures" style={styles.wrap}>
      {departures.map((x) => (
        <WaitingPassengers key={x.id} occurrenceId={x.id} departs={x.depart_earliest} />
      ))}
    </View>
  );
}

const MAX_DEPARTURES = 6;

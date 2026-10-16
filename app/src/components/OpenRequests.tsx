// What you asked for, and what has happened since.
//
// Asking is sequential: one driver at a time, each with an expiry, and nobody
// is obliged to answer. From the passenger's side that looks like nothing
// happening for minutes at a stretch, and the app's only previous answer was a
// pulsing dot on a screen they had to navigate to.
//
// There are four different kinds of nothing here, and they are not the same
// news: nobody asked yet, somebody is reading it now, two people said no and we
// are moving on, or an ask went out and has not come back. Saying which one it
// is costs one request and is the difference between waiting and being
// abandoned.

import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { useQuery } from '@tanstack/react-query';
import { useRouter } from 'expo-router';

import { api, type OpenRequest } from '../api/endpoints';
import { useSession } from '../state/session';
import { Card } from './ui';
import { colors, space, type } from '../design/tokens';
import { JourneySummary } from './JourneySummary';
import { athensTime } from '../design/format';

/**
 * What the caller is waiting on, asked the same way everywhere.
 *
 * Both the card and the home screen's empty state need this, and they need the
 * same answer: two call sites with different options is two different queries
 * as far as anybody reading the code is concerned, and only one of them would
 * have been the one that polls.
 *
 * It polls because nothing here changes because of anything done on this phone.
 * A driver answers somewhere else, and without a timer "ρωτάμε έναν οδηγό τώρα"
 * stays on screen for as long as the app is open — which is the one sentence
 * this card exists to stop being a lie.
 */
export function useOpenRequests() {
  // The tabs mount before the session is settled. Without this the card fires a
  // request with no token on every cold start and shows "δεν φόρτωσαν τα
  // αιτήματά σου" to somebody who is not signed in yet.
  const signedIn = useSession((s) => s.status === 'signedIn');
  return useQuery({
    queryKey: ['requests', 'mine'],
    queryFn: () => api.myRequests(),
    enabled: signedIn,
    refetchInterval: 30_000,
  });
}

export function OpenRequests() {
  const { t } = useTranslation();
  const router = useRouter();

  const requests = useOpenRequests();

  // A list that did not load is not a list with nothing in it, but it is also
  // not worth a red box above somebody's trips: the trips loaded, and they are
  // what the screen is for.
  if (requests.isError) {
    return (
      <Text testID="requests-failed" style={styles.failed}>
        {t('home.requestsFailed')}
      </Text>
    );
  }

  const open = requests.data?.requests ?? [];
  if (open.length === 0) return null;

  return (
    <View testID="open-requests" style={styles.wrap}>
      <Text style={styles.heading}>{t('home.requests')}</Text>
      {open.map((request) => (
        <Card
          key={request.id}
          testID={`request-${request.id}`}
          onPress={() => router.push(`/request/${request.id}`)}
        >
          <JourneySummary
            from={request.origin_label}
            to={request.destination_label}
            start={request.window_start}
            end={request.window_end}
            status={requestProgress(request, t)}
            statusTestID={`request-${request.id}-progress`}
          />
        </Card>
      ))}
    </View>
  );
}

/**
 * One sentence for the state the search is actually in.
 *
 * The count of people who could not comes second, when there is one, because it
 * is the part that explains why this is taking a while — and because "δύο
 * οδηγοί δεν μπορούσαν" on its own reads as a refusal rather than as progress.
 */
export function requestProgress(request: OpenRequest, t: TFunction): string {
  // Somebody whose driver took the seat back has to hear that first, and that
  // the search started again without them doing anything. Otherwise the card
  // reads exactly like a request nobody ever answered.
  if (!request.stranded) return progressOf(request, t);
  // The driver who cancelled was asked, so "waiting for an answer" would be
  // about them. Only a live ask to somebody new is worth adding.
  return request.asking_now
    ? `${t('home.requestStranded')} ${asking(request, t)}`
    : t('home.requestStranded');
}

/**
 * Why nobody has been asked, when the server knows: a car passes but its
 * driver does not take the detour, or nobody leaves in those hours. No driver
 * is named, because none of them agreed to anything (QA 2026-10-16, U3).
 */
function whyNobody(request: OpenRequest, t: TFunction): string | null {
  const m = request.missed;
  if (m?.reason === 'no_stops') return t('home.missedNoStops');
  if (m && m.need != null && m.limit != null) {
    const need = Math.ceil(m.need);
    return m.reason === 'detour_min'
      ? t('home.missedDetourMin', { need, limit: m.limit })
      : t('home.missedDetourKm', { need, limit: m.limit });
  }
  if (request.drivers_departing === 0) {
    return request.nearest_departure
      ? t('home.requestNearest', { time: athensTime(request.nearest_departure) })
      : t('home.requestNotThen');
  }
  return null;
}

/** "Ρωτάμε έναν οδηγό τώρα", and until when they have to answer. */
function asking(request: OpenRequest, t: TFunction): string {
  return request.ask_expires_at
    ? `${t('home.requestAsking')} ${t('home.askAnswersBy', { time: athensTime(request.ask_expires_at) })}`
    : t('home.requestAsking');
}

function progressOf(request: OpenRequest, t: TFunction): string {
  if (request.drivers_asked === 0 && request.drivers_on_corridor === 0)
    return t('home.requestNoDrivers');
  const declined =
    request.drivers_declined > 0
      ? ` ${t('home.requestDeclined', { count: request.drivers_declined })}`
      : '';

  // No car on the road has that many seats: nobody will be asked until one
  // does, and "nobody asked yet" would hide why.
  if (
    request.drivers_asked === 0 &&
    request.most_seats_on_corridor != null &&
    request.most_seats_on_corridor < request.seats
  )
    return t('home.requestTooManySeats', { count: request.most_seats_on_corridor });
  if (request.drivers_asked === 0) {
    const why = whyNobody(request, t);
    if (why) return why;
  }
  if (request.drivers_asked === 0) return t('home.requestNobody');
  if (request.asking_now) return asking(request, t) + declined;
  // Everybody on the road has passed: there is no next driver to look for
  // until somebody publishes. A missing count is not that. The server counts
  // it, because a driver who passed and then retired is in drivers_declined
  // and not on the corridor.
  if (request.drivers_declined > 0 && request.drivers_untried === 0)
    return t('home.requestExhausted');
  if (request.drivers_declined > 0) return t('home.requestSearching') + declined;
  return t('home.requestWaiting');
}

const styles = StyleSheet.create({
  wrap: { gap: space.sm },
  heading: { ...type.caption, color: colors.muted, marginLeft: space.xs },
  failed: { ...type.caption, color: colors.muted, marginLeft: space.xs },
});

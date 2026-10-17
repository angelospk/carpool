import type { TripDetail } from '../api/endpoints';

/** A trip of today or tomorrow with somebody else on it. */
export const LIVE_POLL_MS = 15_000;
/** After the trip, while the other side has still to confirm. */
export const AWAIT_POLL_MS = 30_000;

const athensDate = (at: number) =>
  new Date(at).toLocaleDateString('en-CA', { timeZone: 'Europe/Athens' });
const DAY_MS = 24 * 60 * 60 * 1000;

/**
 * How often the trip screen refetches, or false for not at all.
 *
 * Other people change this screen: a passenger leaves, another is let on, the
 * driver calls it off. Push reaches a phone in the background; a screen left
 * open on a trip of today or tomorrow polls, so the list of who is in the car
 * does not go stale (QA 2026-10-17 U F2). Further off, nothing changes fast
 * enough to be worth the requests.
 */
export function tripPollInterval(trip: TripDetail | undefined, now: number): number | false {
  if (!trip || trip.status === 'cancelled') return false;
  if (awaitingOthers(trip, now)) return AWAIT_POLL_MS;
  const others = trip.role === 'driver' ? trip.passengers.length > 0 : true;
  if (!others) return false;
  const day = athensDate(Date.parse(trip.depart_earliest));
  const soon = day === athensDate(now) || day === athensDate(now + DAY_MS);
  return soon ? LIVE_POLL_MS : false;
}

// A finished trip where the caller has confirmed but somebody they could rate
// has not: rating opens once they confirm too.
function awaitingOthers(trip: TripDetail, now: number): boolean {
  if (!trip.confirmed_by_me) return false;
  if (now < new Date(trip.depart_latest).getTime()) return false;
  const others = trip.role === 'driver' ? trip.passengers : [trip.driver];
  return others.some((p) => !p.can_rate && !p.rated);
}

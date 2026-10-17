import type { TripDetail } from '../api/endpoints';

/** A trip of today or tomorrow. */
export const LIVE_POLL_MS = 15_000;
/** After the trip, while the other side has still to confirm. */
export const AWAIT_POLL_MS = 30_000;

const athensDate = (at: number) =>
  new Date(at).toLocaleDateString('en-CA', { timeZone: 'Europe/Athens' });

/** The calendar day after a YYYY-MM-DD date. Not now + 24 h: a day can be 25. */
function nextDate(date: string): string {
  const [y, m, d] = date.split('-').map(Number);
  return new Date(Date.UTC(y, m - 1, d + 1)).toISOString().slice(0, 10);
}

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
  // A driver's trip with nobody on it yet polls too: its first passenger may
  // be let on from another device.
  const day = athensDate(Date.parse(trip.depart_earliest));
  const today = athensDate(now);
  const soon = day === today || day === nextDate(today);
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

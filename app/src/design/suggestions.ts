// The journeys somebody actually makes, worked out from the ones they made.
//
// There is no recommender behind this and there is not meant to be. A person
// who has ridden Αλεξανδρούπολη → Κομοτηνή four times is going to ride it
// again, and asking them to type both ends for the fifth time is the whole
// friction the home screen is trying to remove.
//
// It is a pure function so that the ranking can be argued with in a test rather
// than by squinting at a screen: what counts as the same journey, which way
// round it goes, and what happens when the data is not there at all.

import { type PastTrip, type TripSummary } from '../api/endpoints';
import { type Pin } from './pin';

/** A journey worth offering again, with the pins to start it from. */
export type Suggestion = {
  /** Stable across renders and unique per direction. Safe as a list key. */
  key: string;
  from: Pin;
  to: Pin;
  /** How many past rides went this way. */
  count: number;
};

/**
 * Rounded to about a hundred metres.
 *
 * Two rides from the same town square are the same journey even when the
 * booking recorded them a few metres apart, and the labels cannot be used for
 * this: they are typed, translated and occasionally "Φέρες, στο βενζινάδικο".
 */
function at(lon: number, lat: number): string {
  return `${lon.toFixed(3)},${lat.toFixed(3)}`;
}

function usable(lon: number, lat: number, label: string): boolean {
  return (
    Number.isFinite(lon) &&
    Number.isFinite(lat) &&
    Math.abs(lon) <= 180 &&
    Math.abs(lat) <= 90 &&
    label.trim().length > 0
  );
}

/** What the two ends of a trip are called, as one string, for comparison. */
function labelled(origin: string, destination: string): string {
  return `${origin.trim()}→${destination.trim()}`;
}

/**
 * The journeys to offer again, most-taken first.
 *
 * Direction matters: the morning run down and the evening run back are two
 * different asks, and merging them would offer somebody a ride home at eight in
 * the morning.
 *
 * Anything already coming up is left out — there is no point suggesting a seat
 * on a journey somebody has already got one for. That exclusion is made on the
 * labels, because an upcoming trip carries no coordinates; it is the looser of
 * the two comparisons and it is deliberately the one used for hiding rather
 * than for grouping.
 */
export function suggestRoutes(
  past: PastTrip[],
  upcoming: TripSummary[],
  limit = 3,
): Suggestion[] {
  const already = new Set(
    upcoming.map((trip) => labelled(trip.origin_label, trip.destination_label)),
  );

  const seen = new Map<string, Suggestion & { last: string }>();
  for (const trip of past) {
    if (!usable(trip.origin_lon, trip.origin_lat, trip.origin_label)) continue;
    if (!usable(trip.destination_lon, trip.destination_lat, trip.destination_label)) continue;
    if (already.has(labelled(trip.origin_label, trip.destination_label))) continue;

    const key = `${at(trip.origin_lon, trip.origin_lat)}>${at(trip.destination_lon, trip.destination_lat)}`;
    const found = seen.get(key);
    if (found) {
      found.count += 1;
      // The most recent labels win, so a journey keeps the name it was last
      // known by rather than the one it had a year ago.
      if (trip.depart_earliest > found.last) {
        found.last = trip.depart_earliest;
        found.from.label = trip.origin_label;
        found.to.label = trip.destination_label;
      }
      continue;
    }
    seen.set(key, {
      key,
      count: 1,
      last: trip.depart_earliest,
      from: { lon: trip.origin_lon, lat: trip.origin_lat, label: trip.origin_label },
      to: { lon: trip.destination_lon, lat: trip.destination_lat, label: trip.destination_label },
    });
  }

  return [...seen.values()]
    // Count, then recency, then the key — so the same history always produces
    // the same list rather than whatever the map iteration happened to give.
    .sort((a, b) => b.count - a.count || (a.last < b.last ? 1 : a.last > b.last ? -1 : 0) || (a.key < b.key ? -1 : 1))
    .slice(0, limit)
    .map(({ key, count, from, to }) => ({ key, count, from, to }));
}

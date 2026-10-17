// When live sharing is open for a trip, worked out on the phone from the
// fields the trip already carries. The numbers are the server's own
// (internal/live/live.go: Before, After, maxRouteTime); the server decides,
// and this only says when to offer what depends on it.

const MIN = 60_000;
const BEFORE = 30 * MIN;
const AFTER = 60 * MIN;
const MAX_ROUTE = 6 * 60 * MIN;

type Departure = { depart_earliest: string; depart_latest: string; route_min?: number };

/** From 30 minutes before the earliest departure to an hour after the latest
    one could arrive. Milliseconds since the epoch. */
export function liveWindow(trip: Departure): { opens: number; closes: number } {
  const routeMin = trip.route_min ?? 0;
  const length = routeMin < 0 || routeMin * MIN > MAX_ROUTE ? MAX_ROUTE : routeMin * MIN;
  return {
    opens: Date.parse(trip.depart_earliest) - BEFORE,
    closes: Date.parse(trip.depart_latest) + length + AFTER,
  };
}

/** Whether `at` is inside the window: the start is in, the end is out. */
export function liveWindowOpen(trip: Departure, at: number): boolean {
  const { opens, closes } = liveWindow(trip);
  return at >= opens && at < closes;
}

// The arrival end of a ticket.
//
// A trip summary carries the departure and the road distance but not how long
// the road takes, so the arrival on a ticket is worked out here and always
// shown with «≈». An even regional pace rather than a motorway one: most of
// these roads are national roads through villages, and a ticket that promises
// too early an arrival is worse than one that is a little late.

/** Kilometres per hour, door to door. */
export const REGIONAL_PACE_KMH = 70;

/** When the car should arrive, rounded up to the next five minutes. */
export function approxArrival(departIso: string, routeKm: number): string {
  const minutes = Math.max(5, Math.ceil(((routeKm / REGIONAL_PACE_KMH) * 60) / 5) * 5);
  return new Date(new Date(departIso).getTime() + minutes * 60_000).toISOString();
}

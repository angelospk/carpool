// Handing a drive to the maps app the driver already uses.
//
// Building turn-by-turn into the app is weeks of work for something worse than
// what is already on the phone, so the app does not: it opens Google Maps,
// Apple Maps or Waze with every stop still ahead — pickups and drop-offs, in
// the order the car meets them — on the way to the end of the driver's road.
// The stops are what make the meeting work: whatever road the maps app picks
// to avoid traffic, it goes through each of them. See specs/DRIVING-NOW.md.

export type MapsApp = 'google' | 'apple' | 'waze';
export const MAPS_APPS: readonly MapsApp[] = ['google', 'apple', 'waze'];

/** [lon, lat], as the server sends points. */
export type LonLat = [number, number];

const latLon = ([lon, lat]: LonLat) => encodeURIComponent(`${lat},${lon}`);

/** The link that opens guidance through the stops, in order, to the end of the road. */
export function mapsUrl(app: MapsApp, to: { stops: LonLat[]; destination: LonLat }): string {
  switch (app) {
    case 'google': {
      const via = to.stops.length ? `&waypoints=${to.stops.map(latLon).join('%7C')}` : '';
      return `https://www.google.com/maps/dir/?api=1&destination=${latLon(to.destination)}${via}&travelmode=driving&dir_action=navigate`;
    }
    case 'apple': {
      const via = to.stops.map((s) => `&waypoint=${latLon(s)}`).join('');
      return `https://maps.apple.com/directions?destination=${latLon(to.destination)}${via}&mode=driving`;
    }
    case 'waze':
      // No waypoints: the next stop, and the rest once that one is done.
      return `https://waze.com/ul?ll=${latLon(to.stops[0] ?? to.destination)}&navigate=yes`;
  }
}

/**
 * The pickups and drop-offs still ahead, in the order the car meets them.
 *
 * Each point is placed on the nearest segment of the road, as how many
 * segments along plus how far into that one, which is plenty to order a
 * handful of stops along one route. With the car's position, anything at or
 * behind it is done; without one, nothing is.
 */
export function remainingStops(
  road: LonLat[],
  car: LonLat | null,
  passengers: { pickup?: LonLat | null; dropoff?: LonLat | null }[],
): LonLat[] {
  const passed = car ? along(road, car) : -Infinity;
  return passengers
    .flatMap((p) => [p.pickup, p.dropoff])
    .filter((s): s is LonLat => !!s)
    .map((s) => ({ s, at: along(road, s) }))
    .filter(({ at }) => at > passed)
    .sort((x, y) => x.at - y.at)
    .map(({ s }) => s);
}

/** How far along the road a point lies: segment index plus the fraction into it. */
function along(road: LonLat[], p: LonLat): number {
  if (road.length < 2) return 0;
  let best = 0;
  let bestD = Infinity;
  for (let i = 0; i < road.length - 1; i++) {
    const [a, b] = [road[i], road[i + 1]];
    const dx = b[0] - a[0];
    const dy = b[1] - a[1];
    const len = dx * dx + dy * dy;
    const t = len === 0 ? 0 : Math.max(0, Math.min(1, ((p[0] - a[0]) * dx + (p[1] - a[1]) * dy) / len));
    const d = (a[0] + t * dx - p[0]) ** 2 + (a[1] + t * dy - p[1]) ** 2;
    if (d < bestD) {
      bestD = d;
      best = i + t;
    }
  }
  return best;
}

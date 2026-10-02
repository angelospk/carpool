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
 * The pickups and drop-offs not yet visited, in the order the car meets them
 * along the road.
 *
 * Whether a stop was visited is not guessed from where the car is now — a
 * pickup off to the side looks the same as one just made — but recorded as the
 * drive goes: the phone marks a stop once one of its own fixes came within
 * VISIT_M of it (see markVisited). A phone that recorded nothing drops nothing,
 * which at worst sends the driver past a stop already made, never past one
 * still to make.
 */
export function remainingStops(
  road: LonLat[],
  passengers: { pickup?: LonLat | null; dropoff?: LonLat | null }[],
  visited: ReadonlySet<string>,
): LonLat[] {
  return passengers
    .flatMap((p) => [p.pickup, p.dropoff])
    .filter((s): s is LonLat => !!s && !visited.has(stopKey(s)))
    .map((s) => ({ s, at: metresAlong(road, s) }))
    .sort((x, y) => x.at - y.at)
    .map(({ s }) => s);
}

/** A stop's identity: its point, to the metre. */
export const stopKey = ([lon, lat]: LonLat) => `${lon.toFixed(5)},${lat.toFixed(5)}`;

/**
 * The stops the car has been at: those any of these fixes came within VISIT_M
 * of, added to the ones already recorded. Fixes come at most 150 m apart, so a
 * car passing a stop on its road always has one within 75 m; a pickup off to
 * the side is only reached by going to it.
 */
export function markVisited(fixes: LonLat[], stops: LonLat[], visited: ReadonlySet<string>): Set<string> {
  const out = new Set(visited);
  for (const s of stops) {
    if (fixes.some((f) => metres(f, s) <= VISIT_M)) out.add(stopKey(s));
  }
  return out;
}

const VISIT_M = 80;

/** Metres between two nearby points; flat-earth is exact enough within a region. */
function metres(a: LonLat, b: LonLat): number {
  const k = Math.cos(((a[1] + b[1]) / 2) * (Math.PI / 180));
  return Math.hypot((b[0] - a[0]) * k, b[1] - a[1]) * 111_320;
}

/** How far along the road a point lies, in metres, through its nearest segment. */
function metresAlong(road: LonLat[], p: LonLat): number {
  if (road.length < 2) return 0;
  let best = 0;
  let bestD = Infinity;
  let before = 0;
  for (let i = 0; i < road.length - 1; i++) {
    const [a, b] = [road[i], road[i + 1]];
    const k = Math.cos(a[1] * (Math.PI / 180));
    const dx = (b[0] - a[0]) * k;
    const dy = b[1] - a[1];
    const len = dx * dx + dy * dy;
    const t = len === 0 ? 0 : Math.max(0, Math.min(1, ((p[0] - a[0]) * k * dx + (p[1] - a[1]) * dy) / len));
    const d = ((a[0] - p[0]) * k + t * dx) ** 2 + (a[1] - p[1] + t * dy) ** 2;
    const segment = metres(a, b);
    if (d < bestD) {
      bestD = d;
      best = before + t * segment;
    }
    before += segment;
  }
  return best;
}

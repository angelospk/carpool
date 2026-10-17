// Where the car probably is between two reports of where it was.
//
// The driver's phone reports every 15 to 30 seconds, which is far less often
// than a passenger watches the screen. Redrawing the dot only when a report
// arrives makes it jump; sending reports faster costs the driver's battery.
// This moves the dot along the route from the last report, at the speed the
// last two reports imply, and stops when a guess would be worse than the last
// known place.
//
// Pure, so the maths can be tested without a map or a clock. Points are
// [lat, lon], as @mapbox/polyline decodes them.

export type LatLon = [number, number];

/** A reported position and when it was taken, in epoch milliseconds. */
export type TimedFix = { lat: number; lon: number; at: number };

/** Past this the car may be anywhere: the dot greys and stops. */
export const STALE_MS = 2 * 60_000;
/** The longest a dot is moved on a guess before the next report. */
export const MAX_EXTRAPOLATION_MS = 30_000;
/** A report further than this from the route is not on it, and is shown as it is. */
export const OFF_ROUTE_M = 150;
/** Faster than this (about 144 km/h) is a bad pair of reports, not a car. */
const MAX_SPEED_MS = 40;

const M_PER_DEG_LAT = 111_320;

// Metres east and north of `origin`. Flat-earth maths: along a few kilometres
// of road the error is far below what a GPS fix has.
function local(origin: LatLon, p: LatLon): [number, number] {
  const cos = Math.cos((origin[0] * Math.PI) / 180);
  return [(p[1] - origin[1]) * cos * M_PER_DEG_LAT, (p[0] - origin[0]) * M_PER_DEG_LAT];
}

/** Metres between two points. */
export function metres(a: LatLon, b: LatLon): number {
  const [x, y] = local(a, b);
  return Math.hypot(x, y);
}

/** Cumulative length of the line at each of its vertices, in metres. */
function cumulative(line: LatLon[]): number[] {
  const out = [0];
  for (let i = 1; i < line.length; i++) out.push(out[i - 1] + metres(line[i - 1], line[i]));
  return out;
}

/**
 * The nearest point on the line to `p`: how far along the line it is, and how
 * far `p` is from the line. Null for a line with fewer than two points.
 */
export function projectOnPolyline(
  line: LatLon[],
  p: LatLon,
): { along: number; offRoute: number } | null {
  if (line.length < 2) return null;
  const lengths = cumulative(line);
  let best: { along: number; offRoute: number } | null = null;
  for (let i = 1; i < line.length; i++) {
    const a = line[i - 1];
    const [bx, by] = local(a, line[i]);
    const [px, py] = local(a, p);
    const len2 = bx * bx + by * by;
    const t = len2 === 0 ? 0 : Math.max(0, Math.min(1, (px * bx + py * by) / len2));
    const offRoute = Math.hypot(px - t * bx, py - t * by);
    if (!best || offRoute < best.offRoute) {
      best = { along: lengths[i - 1] + t * Math.sqrt(len2), offRoute };
    }
  }
  return best;
}

/** The point `along` metres from the start of the line, clamped to its ends. */
export function pointAlong(line: LatLon[], along: number): LatLon | null {
  if (line.length === 0) return null;
  if (line.length === 1 || along <= 0) return line[0];
  const lengths = cumulative(line);
  if (along >= lengths[lengths.length - 1]) return line[line.length - 1];
  for (let i = 1; i < line.length; i++) {
    if (along <= lengths[i]) {
      const span = lengths[i] - lengths[i - 1];
      const t = span === 0 ? 0 : (along - lengths[i - 1]) / span;
      return [
        line[i - 1][0] + (line[i][0] - line[i - 1][0]) * t,
        line[i - 1][1] + (line[i][1] - line[i - 1][1]) * t,
      ];
    }
  }
  return line[line.length - 1];
}

/** Whether a report taken at `at` is too old to be moved or trusted. */
export function isStale(at: number, now: number): boolean {
  return now - at > STALE_MS;
}

export type Shown = { lat: number; lon: number; stale: boolean };

/**
 * Where to draw the car at `now`.
 *
 * `last` is the newest report and `prev` the one before it, if there was one.
 * The car is moved from `last` along `line` by the speed between the two, for
 * the time since `last` and no more than MAX_EXTRAPOLATION_MS. It stays where
 * it was reported when: there is no earlier report or no route, the reports
 * are not along the route in the direction of travel, either is off the route,
 * or the report is stale — and then it is also marked stale.
 */
export function interpolate(
  line: LatLon[],
  last: TimedFix,
  prev: TimedFix | null,
  now: number,
): Shown {
  const asReported = { lat: last.lat, lon: last.lon };
  if (isStale(last.at, now)) return { ...asReported, stale: true };
  if (!prev || line.length < 2 || last.at <= prev.at) return { ...asReported, stale: false };

  const from = projectOnPolyline(line, [prev.lat, prev.lon]);
  const to = projectOnPolyline(line, [last.lat, last.lon]);
  if (!from || !to || from.offRoute > OFF_ROUTE_M || to.offRoute > OFF_ROUTE_M) {
    return { ...asReported, stale: false };
  }
  const speed = (to.along - from.along) / ((last.at - prev.at) / 1000);
  if (!(speed > 0) || speed > MAX_SPEED_MS) return { ...asReported, stale: false };

  const elapsed = Math.min(Math.max(0, now - last.at), MAX_EXTRAPOLATION_MS) / 1000;
  const at = pointAlong(line, to.along + speed * elapsed);
  return at ? { lat: at[0], lon: at[1], stale: false } : { ...asReported, stale: false };
}

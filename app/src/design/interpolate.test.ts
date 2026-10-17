import {
  type LatLon,
  type TimedFix,
  MAX_EXTRAPOLATION_MS,
  STALE_MS,
  interpolate,
  isStale,
  metres,
  pointAlong,
  projectOnPolyline,
} from './interpolate';

// A road running due east along 40.85 N, about 8.4 km long, with a bend north
// at the end.
const east: LatLon[] = [
  [40.85, 25.8],
  [40.85, 25.85],
  [40.85, 25.9],
  [40.88, 25.9],
];
const lon = (metresEast: number) => 25.8 + metresEast / (111_320 * Math.cos((40.85 * Math.PI) / 180));

describe('projectOnPolyline', () => {
  test('a point on the road is as far along as it is from the start', () => {
    const p = projectOnPolyline(east, [40.85, lon(1000)])!;
    expect(p.along).toBeCloseTo(1000, -1);
    expect(p.offRoute).toBeLessThan(1);
  });

  test('a point beside the road projects onto it and reports the distance', () => {
    const p = projectOnPolyline(east, [40.8509, lon(1000)])!; // ~100 m north
    expect(p.along).toBeCloseTo(1000, -1);
    expect(p.offRoute).toBeGreaterThan(90);
    expect(p.offRoute).toBeLessThan(110);
  });

  test('past either end it clamps to the end', () => {
    expect(projectOnPolyline(east, [40.85, 25.7])!.along).toBe(0);
    const total = projectOnPolyline(east, east[3])!.along;
    expect(projectOnPolyline(east, [40.9, 25.9])!.along).toBeCloseTo(total, 3);
  });

  test('a line without two points has nothing to project onto', () => {
    expect(projectOnPolyline([], [40, 25])).toBeNull();
    expect(projectOnPolyline([[40, 25]], [40, 25])).toBeNull();
  });
});

describe('pointAlong', () => {
  test('walks round a bend', () => {
    const toBend = metres(east[0], east[2]);
    const p = pointAlong(east, toBend + 1000)!;
    expect(p[1]).toBeCloseTo(25.9, 6);
    expect(metres(east[2], p)).toBeCloseTo(1000, -1);
  });

  test('clamps to the ends', () => {
    expect(pointAlong(east, -5)).toEqual(east[0]);
    expect(pointAlong(east, 1e9)).toEqual(east[3]);
    expect(pointAlong([], 5)).toBeNull();
  });
});

describe('isStale', () => {
  test('two minutes is fresh, a moment more is not', () => {
    expect(isStale(0, STALE_MS)).toBe(false);
    expect(isStale(0, STALE_MS + 1)).toBe(true);
  });
});

describe('interpolate', () => {
  // 20 m/s east: 600 m in the 30 s between the reports.
  const prev: TimedFix = { lat: 40.85, lon: lon(1000), at: 0 };
  const last: TimedFix = { lat: 40.85, lon: lon(1600), at: 30_000 };

  test('moves along the route at the last speed', () => {
    const shown = interpolate(east, last, prev, 40_000);
    expect(shown.stale).toBe(false);
    expect(metres([shown.lat, shown.lon], [last.lat, last.lon])).toBeCloseTo(200, -1);
    expect(shown.lon).toBeGreaterThan(last.lon);
  });

  test('at the moment of the report it is where it was reported', () => {
    const shown = interpolate(east, last, prev, 30_000);
    expect(shown.lon).toBeCloseTo(last.lon, 6);
  });

  test('stops guessing after 30 seconds', () => {
    const capped = interpolate(east, last, prev, 30_000 + MAX_EXTRAPOLATION_MS);
    const later = interpolate(east, last, prev, 30_000 + MAX_EXTRAPOLATION_MS + 60_000);
    expect(later).toEqual(capped);
    expect(metres([capped.lat, capped.lon], [last.lat, last.lon])).toBeCloseTo(600, -1);
  });

  test('follows the bend rather than a straight line', () => {
    const nearBend: TimedFix = { lat: 40.85, lon: lon(metres(east[0], east[2]) - 100), at: 30_000 };
    const before: TimedFix = { lat: 40.85, lon: lon(metres(east[0], east[2]) - 700), at: 0 };
    const shown = interpolate(east, nearBend, before, 30_000 + 25_000); // 500 m on
    expect(shown.lon).toBeCloseTo(25.9, 6);
    expect(shown.lat).toBeGreaterThan(40.85);
  });

  test('a report older than two minutes is greyed and does not move', () => {
    const shown = interpolate(east, last, prev, last.at + STALE_MS + 1);
    expect(shown).toEqual({ lat: last.lat, lon: last.lon, stale: true });
  });

  test('without an earlier report there is no speed, so no movement', () => {
    expect(interpolate(east, last, null, 40_000)).toEqual({ lat: last.lat, lon: last.lon, stale: false });
  });

  test('a car that went backwards or stood still is not moved', () => {
    const back = interpolate(east, prev, last, 100_000);
    expect(back).toEqual({ lat: prev.lat, lon: prev.lon, stale: false });
    const still = interpolate(east, last, { ...last, at: 0 }, 40_000);
    expect(still).toEqual({ lat: last.lat, lon: last.lon, stale: false });
  });

  test('a report off the route is shown as it is', () => {
    const off: TimedFix = { lat: 40.86, lon: lon(1600), at: 30_000 };
    expect(interpolate(east, off, prev, 40_000)).toEqual({ lat: 40.86, lon: off.lon, stale: false });
  });

  test('without a route it is shown as reported', () => {
    expect(interpolate([], last, prev, 40_000)).toEqual({ lat: last.lat, lon: last.lon, stale: false });
  });

  test('an absurd speed is a bad pair of reports, not a car', () => {
    const jump: TimedFix = { lat: 40.85, lon: lon(1600), at: 1000 };
    expect(interpolate(east, jump, prev, 11_000)).toEqual({ lat: jump.lat, lon: jump.lon, stale: false });
  });
});

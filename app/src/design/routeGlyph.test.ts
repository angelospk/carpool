import { glyphPath } from './routeGlyph';

const box = { width: 100, height: 60, padding: 6 };

// A shape drawn from raw lat/lon is unreadable: degrees of longitude are not
// degrees of latitude, and Evros is a few hundredths of a degree wide. The glyph
// normalizes into its box instead of mapping coordinates absolutely.
test('fits the whole route inside the box', () => {
  const path = glyphPath(
    [
      [40.85, 25.87],
      [41.12, 25.4],
      [41.12, 25.87],
    ],
    box,
  );

  const coords = [...path.matchAll(/(-?[\d.]+) (-?[\d.]+)/g)].map(([, x, y]) => [+x, +y]);
  expect(coords).toHaveLength(3);
  for (const [x, y] of coords) {
    expect(x).toBeGreaterThanOrEqual(box.padding);
    expect(x).toBeLessThanOrEqual(box.width - box.padding);
    expect(y).toBeGreaterThanOrEqual(box.padding);
    expect(y).toBeLessThanOrEqual(box.height - box.padding);
  }
});

test('draws north upwards', () => {
  const path = glyphPath(
    [
      [40.8, 25.0],
      [41.2, 25.0],
    ],
    box,
  );

  const [[, southY], [, northY]] = [...path.matchAll(/(-?[\d.]+) (-?[\d.]+)/g)].map(([, x, y]) => [
    +x,
    +y,
  ]);
  expect(northY).toBeLessThan(southY);
});

// Squashing a mostly-east-west route to fill a tall box would make every route
// look the same. The glyph keeps the shape and centres it instead.
test('keeps the aspect ratio and centres what is left over', () => {
  const path = glyphPath(
    [
      [41.0, 25.0],
      [41.0, 26.0],
    ],
    box,
  );

  const ys = [...path.matchAll(/(-?[\d.]+) (-?[\d.]+)/g)].map(([, , y]) => +y);
  expect(ys[0]).toBeCloseTo(box.height / 2, 5);
  expect(ys[1]).toBeCloseTo(box.height / 2, 5);
});

test('a single point or none draws nothing', () => {
  expect(glyphPath([[41.0, 25.0]], box)).toBe('');
  expect(glyphPath([], box)).toBe('');
});

// A route that doubles back on itself has zero extent in one axis; dividing by
// that span would put every point at NaN and the card would render blank.
test('survives a route with no width', () => {
  const path = glyphPath(
    [
      [41.0, 25.0],
      [41.1, 25.0],
    ],
    box,
  );

  expect(path).not.toContain('NaN');
});

// OSRM returns hundreds of points for a 60 km route. Drawing them all costs
// nothing visible at this size, so the glyph thins them out.
test('thins a long route down', () => {
  const many = Array.from({ length: 500 }, (_, i) => [41 + i / 1000, 25 + i / 800] as [number, number]);

  const commands = glyphPath(many, box).match(/[ML]/g) ?? [];

  expect(commands.length).toBeLessThanOrEqual(48);
  expect(commands.length).toBeGreaterThan(8);
});

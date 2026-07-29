// The route glyph: the real shape of a route, drawn as a small vector mark.
//
// The pilot ships without map tiles — there is no key to hold and no vendor to
// pay, and a tile view of rural Thrace tells a rider nothing they do not already
// know. But the *shape* of a route is recognisable at a glance: the road to
// Komotini bends differently from the road to Orestiada. So every trip carries
// its own geometry as a mark, drawn from the polyline the server already sends.
//
// This file is pure geometry so it can be tested without rendering anything.

export type LatLon = [lat: number, lon: number];

export type GlyphBox = {
  width: number;
  height: number;
  /** Keeps the stroke and its round caps off the edge of the box. */
  padding: number;
};

/** Above this the extra points are invisible at glyph size, so they are dropped. */
const maxPoints = 48;

/**
 * Returns an SVG path for a route, normalized into box.
 *
 * The route is fitted rather than mapped absolutely: routes span hundredths of a
 * degree, and a degree of longitude is not a degree of latitude, so drawing raw
 * coordinates would produce a dot. The fit preserves the aspect ratio — a
 * squashed shape would make every route look alike — and centres the remainder.
 */
export function glyphPath(points: readonly LatLon[], box: GlyphBox): string {
  const pts = thin(points);
  if (pts.length < 2) return '';

  const lats = pts.map(([lat]) => lat);
  const lons = pts.map(([, lon]) => lon);
  const minLat = Math.min(...lats);
  const maxLat = Math.max(...lats);
  const minLon = Math.min(...lons);
  const maxLon = Math.max(...lons);

  const inner = { w: box.width - 2 * box.padding, h: box.height - 2 * box.padding };
  const spanLon = maxLon - minLon;
  const spanLat = maxLat - minLat;
  // A route running due north has no width at all, and dividing by that span
  // would put every point at NaN. Only the axes that actually have extent
  // constrain the scale; a flat axis then falls to the centre line below, which
  // is what a straight north-south route should look like.
  const fits = [
    ...(spanLon > 0 ? [inner.w / spanLon] : []),
    ...(spanLat > 0 ? [inner.h / spanLat] : []),
  ];
  const scale = fits.length ? Math.min(...fits) : 1;

  const offsetX = box.padding + (inner.w - spanLon * scale) / 2;
  const offsetY = box.padding + (inner.h - spanLat * scale) / 2;

  return pts
    .map(([lat, lon], i) => {
      const x = offsetX + (lon - minLon) * scale;
      // SVG y grows downwards; latitude grows north. Flipping keeps north up.
      const y = offsetY + (maxLat - lat) * scale;
      return `${i === 0 ? 'M' : 'L'}${round(x)} ${round(y)}`;
    })
    .join(' ');
}

/** The first and last points are kept whatever happens: they are the endpoints. */
function thin(points: readonly LatLon[]): readonly LatLon[] {
  if (points.length <= maxPoints) return points;
  const step = (points.length - 1) / (maxPoints - 1);
  return Array.from({ length: maxPoints }, (_, i) => points[Math.round(i * step)]);
}

function round(n: number): number {
  return Math.round(n * 100) / 100;
}

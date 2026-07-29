// A pin is a coordinate plus the label the user recognises it by.
//
// While the pilot runs without a geocoder, both are typed as "lon, lat Label".
// A map picker replaces this without changing the shape it produces — which is
// the reason the parsing lives here rather than inside a screen.

export type Pin = { lon: number; lat: number; label: string };

const PATTERN = /^\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*(.*)$/;

export function parsePin(text: string): Pin | null {
  const m = text.match(PATTERN);
  if (!m) return null;

  const lon = Number(m[1]);
  const lat = Number(m[2]);
  // Refusing impossible coordinates here means the field can show it is wrong
  // while the user is still looking at it, instead of after a round trip.
  if (Math.abs(lon) > 180 || Math.abs(lat) > 90) return null;

  return { lon, lat, label: m[3].trim() || text.trim() };
}

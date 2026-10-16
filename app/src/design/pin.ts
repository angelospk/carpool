// A pin is a coordinate plus the label the user recognises it by.
//
// While the pilot runs without a geocoder, both are typed as "lon, lat Label".
// A map picker replaces this without changing the shape it produces — which is
// the reason the parsing lives here rather than inside a screen.

export type Pin = {
  lon: number;
  lat: number;
  label: string;
  /**
   * The settlement this came from, when it came from the list. Absent for a
   * coordinate somebody typed, which belongs to no settlement and so has no
   * meeting points to offer.
   */
  id?: string;
};

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

/**
 * The two pins carried in a link, or nothing.
 *
 * The home screen offers a journey somebody already makes and the search screen
 * has to open with both ends filled in. Route params are the only channel
 * between them, and everything that arrives that way is a string, an array of
 * strings, or missing — expo-router gives no guarantee about which.
 *
 * Both ends or neither. Half a preset fills half a form, and a stale second pin
 * beside a fresh first one is a request to somewhere nobody asked to go.
 */
export function pinsFromParams(
  params: Record<string, string | string[] | undefined>,
): { from: Pin; to: Pin } | null {
  const from = pinFromParams(params, 'from');
  const to = pinFromParams(params, 'to');
  return from && to ? { from, to } : null;
}

function pinFromParams(
  params: Record<string, string | string[] | undefined>,
  which: 'from' | 'to',
): Pin | null {
  const lon = Number(one(params[`${which}_lon`]));
  const lat = Number(one(params[`${which}_lat`]));
  const label = (one(params[`${which}_label`]) ?? '').trim();

  if (!Number.isFinite(lon) || !Number.isFinite(lat)) return null;
  if (Math.abs(lon) > 180 || Math.abs(lat) > 90) return null;
  if (!label) return null;

  return { lon, lat, label };
}

// A repeated query parameter arrives as an array. The first is what was meant;
// guessing between two is worse than taking the one that was written first.
function one(value: string | string[] | undefined): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}

/**
 * "Φέρες, ΚΤΕΛ Έβρου" rather than either alone: the town is what somebody
 * recognises in a list and the landmark is what they go to.
 */
export function pointLabel(pin: Pin, point: { name: string } | null): string {
  return point ? `${pin.label}, ${point.name}` : pin.label;
}

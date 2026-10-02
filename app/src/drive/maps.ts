// Handing a drive to the maps app the driver already uses.
//
// Building turn-by-turn into the app is weeks of work for something worse than
// what is already on the phone, so the app does not: it opens Google Maps,
// Apple Maps or Waze with the pickup as a stop on the way and the end of the
// driver's road as the destination. The stop is what makes the meeting work —
// whatever road the maps app picks to avoid traffic, it goes through the
// pickup. See specs/DRIVING-NOW.md.

export type MapsApp = 'google' | 'apple' | 'waze';
export const MAPS_APPS: readonly MapsApp[] = ['google', 'apple', 'waze'];

/** [lon, lat], as the server sends points. */
export type LonLat = [number, number];

const latLon = ([lon, lat]: LonLat) => encodeURIComponent(`${lat},${lon}`);

/** The link that opens guidance: through the pickup, if any, to the end of the road. */
export function mapsUrl(app: MapsApp, to: { pickup?: LonLat; destination: LonLat }): string {
  switch (app) {
    case 'google': {
      const via = to.pickup ? `&waypoints=${latLon(to.pickup)}` : '';
      return `https://www.google.com/maps/dir/?api=1&destination=${latLon(to.destination)}${via}&travelmode=driving&dir_action=navigate`;
    }
    case 'apple': {
      const via = to.pickup ? `&waypoint=${latLon(to.pickup)}` : '';
      return `https://maps.apple.com/directions?destination=${latLon(to.destination)}${via}&mode=driving`;
    }
    case 'waze':
      // No waypoints: the pickup first, the destination once they are in.
      return `https://waze.com/ul?ll=${latLon(to.pickup ?? to.destination)}&navigate=yes`;
  }
}

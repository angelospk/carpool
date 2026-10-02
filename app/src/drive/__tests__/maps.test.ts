import { mapsUrl } from '../maps';

const pickup: [number, number] = [25.9, 40.85];
const end: [number, number] = [26.0, 40.86];

// Google takes the pickup as a waypoint and the end of the road as the
// destination, and starts guidance.
test('google stops at the pickup on the way to the end of the road', () => {
  expect(mapsUrl('google', { pickup, destination: end })).toBe(
    'https://www.google.com/maps/dir/?api=1&destination=40.86%2C26&waypoints=40.85%2C25.9&travelmode=driving&dir_action=navigate',
  );
});

test('apple maps takes the pickup as a waypoint', () => {
  expect(mapsUrl('apple', { pickup, destination: end })).toBe(
    'https://maps.apple.com/directions?destination=40.86%2C26&waypoint=40.85%2C25.9&mode=driving',
  );
});

// Waze has no waypoints: it goes to the pickup, and the end of the road is
// offered again once the passenger is in.
test('waze goes to the pickup first', () => {
  expect(mapsUrl('waze', { pickup, destination: end })).toBe(
    'https://waze.com/ul?ll=40.85%2C25.9&navigate=yes',
  );
  expect(mapsUrl('waze', { destination: end })).toBe('https://waze.com/ul?ll=40.86%2C26&navigate=yes');
});

test('with nobody to pick up it is just the end of the road', () => {
  expect(mapsUrl('google', { destination: end })).toBe(
    'https://www.google.com/maps/dir/?api=1&destination=40.86%2C26&travelmode=driving&dir_action=navigate',
  );
});

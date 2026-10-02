import { mapsUrl, remainingStops } from '../maps';

const a: [number, number] = [25.9, 40.85];
const b: [number, number] = [25.95, 40.85];
const end: [number, number] = [26.0, 40.86];

// Every stop still ahead, in the order the car meets them, then the end of
// the road: a second passenger must not drop the first one's pickup.
test('google stops at every stop on the way to the end of the road', () => {
  expect(mapsUrl('google', { stops: [a, b], destination: end })).toBe(
    'https://www.google.com/maps/dir/?api=1&destination=40.86%2C26&waypoints=40.85%2C25.9%7C40.85%2C25.95&travelmode=driving&dir_action=navigate',
  );
});

test('apple maps takes each stop as a waypoint', () => {
  expect(mapsUrl('apple', { stops: [a, b], destination: end })).toBe(
    'https://maps.apple.com/directions?destination=40.86%2C26&waypoint=40.85%2C25.9&waypoint=40.85%2C25.95&mode=driving',
  );
});

// Waze has no waypoints: the next stop, and the rest once that one is done.
test('waze goes to the next stop', () => {
  expect(mapsUrl('waze', { stops: [a, b], destination: end })).toBe(
    'https://waze.com/ul?ll=40.85%2C25.9&navigate=yes',
  );
  expect(mapsUrl('waze', { stops: [], destination: end })).toBe(
    'https://waze.com/ul?ll=40.86%2C26&navigate=yes',
  );
});

test('with nothing left to stop for it is just the end of the road', () => {
  expect(mapsUrl('google', { stops: [], destination: end })).toBe(
    'https://www.google.com/maps/dir/?api=1&destination=40.86%2C26&travelmode=driving&dir_action=navigate',
  );
});

const road: [number, number][] = [
  [25.8, 40.85],
  [25.85, 40.85],
  [25.9, 40.85],
  [25.95, 40.85],
  [26.0, 40.85],
];

// Pickups and drop-offs along the road, ordered as the car meets them; what
// the car has already passed is left out.
test('the stops left are the ones ahead of the car, in road order', () => {
  const passengers = [
    { pickup: [25.86, 40.85] as [number, number], dropoff: [25.99, 40.85] as [number, number] },
    { pickup: [25.94, 40.85] as [number, number], dropoff: [25.97, 40.85] as [number, number] },
  ];
  expect(remainingStops(road, null, passengers)).toEqual([
    [25.86, 40.85],
    [25.94, 40.85],
    [25.97, 40.85],
    [25.99, 40.85],
  ]);
  // The car is past the first pickup: that one is done, its drop-off is not.
  expect(remainingStops(road, [25.9, 40.85], passengers)).toEqual([
    [25.94, 40.85],
    [25.97, 40.85],
    [25.99, 40.85],
  ]);
});

import { mapsUrl, markVisited, remainingStops, stopKey } from '../maps';

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

const first = { pickup: [25.86, 40.85] as [number, number], dropoff: [25.99, 40.85] as [number, number] };
const second = { pickup: [25.94, 40.85] as [number, number], dropoff: [25.97, 40.85] as [number, number] };

// Every stop not yet visited, in the order the car meets them along the road.
test('the stops left are the ones not visited, in road order', () => {
  expect(remainingStops(road, [first, second], new Set())).toEqual([
    [25.86, 40.85],
    [25.94, 40.85],
    [25.97, 40.85],
    [25.99, 40.85],
  ]);
  expect(remainingStops(road, [first, second], new Set([stopKey([25.86, 40.85])]))).toEqual([
    [25.94, 40.85],
    [25.97, 40.85],
    [25.99, 40.85],
  ]);
});

// The drive's own fixes, at most 150 m apart, record a stop on the road as the
// car passes it; a pickup 100 m off to the side is not visited by driving by.
test('a stop is visited when a fix came close to it, not by driving level with it', () => {
  const onRoad: [number, number] = [25.9, 40.85];
  const aside: [number, number] = [25.92, 40.8509]; // ~100 m off the road
  const fixes: [number, number][] = [
    [25.8991, 40.85],
    [25.9009, 40.85], // ~75 m either side of the stop on the road
    [25.92, 40.85],
  ];
  const visited = markVisited(fixes, [onRoad, aside], new Set());
  expect(visited.has(stopKey(onRoad))).toBe(true);
  expect(visited.has(stopKey(aside))).toBe(false);
  // Once recorded, a stop stays visited however far the car goes.
  expect(markVisited([[26.0, 40.85]], [onRoad], visited).has(stopKey(onRoad))).toBe(true);
});

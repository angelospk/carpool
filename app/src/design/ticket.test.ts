import { approxArrival } from './ticket';

test('arrival is the departure plus the distance at a regional pace, rounded up to five minutes', () => {
  // 57.9 km at 70 km/h is 49.6 minutes, which a ticket says as fifty.
  expect(approxArrival('2026-10-16T04:30:00.000Z', 57.9)).toBe('2026-10-16T05:20:00.000Z');
});

test('a very short hop still takes five minutes', () => {
  expect(approxArrival('2026-10-16T04:30:00.000Z', 0.4)).toBe('2026-10-16T04:35:00.000Z');
});

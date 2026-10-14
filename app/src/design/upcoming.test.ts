import { thisWeek } from './upcoming';

const now = new Date('2026-10-14T06:00:00Z');
const at = (id: string, iso: string, role: 'driver' | 'passenger' = 'driver') => ({
  id,
  role,
  depart_earliest: iso,
});

// One recurring weekday route is fifteen departures over the three weeks the
// server materialises. Listing them all buried the one trip somebody had booked.
test('keeps the coming seven days', () => {
  const { shown, later } = thisWeek(
    [at('a', '2026-10-15T04:30:00Z'), at('b', '2026-10-21T04:30:00Z'), at('c', '2026-10-22T04:30:00Z')],
    now,
  );
  expect(shown.map((x) => x.id)).toEqual(['a', 'b']);
  expect(later).toBe(1);
});

test('a seat somebody has booked is never hidden, however far away', () => {
  const { shown, later } = thisWeek(
    [at('a', '2026-10-15T04:30:00Z'), at('far', '2026-11-01T04:30:00Z', 'passenger')],
    now,
  );
  expect(shown.map((x) => x.id)).toEqual(['a', 'far']);
  expect(later).toBe(0);
});

test('keeps the order it was given', () => {
  const { shown } = thisWeek([at('x', '2026-10-16T04:30:00Z'), at('y', '2026-10-15T04:30:00Z')], now);
  expect(shown.map((x) => x.id)).toEqual(['x', 'y']);
});

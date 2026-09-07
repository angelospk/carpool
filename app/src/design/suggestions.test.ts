import { suggestRoutes } from './suggestions';
import { type PastTrip, type TripSummary } from '../api/endpoints';

const trip = (over: Partial<PastTrip> = {}): PastTrip => ({
  id: 't1',
  booking_id: 'b1',
  role: 'passenger',
  depart_earliest: '2026-07-01T04:30:00Z',
  depart_latest: '2026-07-01T05:00:00Z',
  status: 'completed',
  route_km: 57.8,
  origin_label: 'Αλεξανδρούπολη',
  destination_label: 'Κομοτηνή',
  price_eur: 2.5,
  seats_free: 2,
  driver_name: 'Οδηγός',
  origin_lon: 25.8743,
  origin_lat: 40.8476,
  destination_lon: 25.4054,
  destination_lat: 41.1223,
  ...over,
});

const upcoming = (over: Partial<TripSummary> = {}): TripSummary => ({
  id: 'u1',
  role: 'passenger',
  depart_earliest: '2026-08-01T04:30:00Z',
  depart_latest: '2026-08-01T05:00:00Z',
  status: 'confirmed',
  route_km: 57.8,
  origin_label: 'Αλεξανδρούπολη',
  destination_label: 'Κομοτηνή',
  price_eur: 2.5,
  seats_free: 2,
  ...over,
});

test('the same journey twice is one suggestion, counted', () => {
  const got = suggestRoutes([trip(), trip({ id: 't2', booking_id: 'b2' })], []);

  expect(got).toHaveLength(1);
  expect(got[0].count).toBe(2);
  expect(got[0].from.label).toBe('Αλεξανδρούπολη');
  expect(got[0].to.label).toBe('Κομοτηνή');
});

// A few metres apart is the same square, not a second journey.
test('nearby endpoints are the same journey', () => {
  const got = suggestRoutes(
    [trip(), trip({ id: 't2', booking_id: 'b2', origin_lon: 25.87438, origin_lat: 40.84762 })],
    [],
  );

  expect(got).toHaveLength(1);
  expect(got[0].count).toBe(2);
});

// The morning run down and the evening run back are two different asks.
test('the way back is a separate suggestion', () => {
  const back = trip({
    id: 't2',
    booking_id: 'b2',
    origin_label: 'Κομοτηνή',
    destination_label: 'Αλεξανδρούπολη',
    origin_lon: 25.4054,
    origin_lat: 41.1223,
    destination_lon: 25.8743,
    destination_lat: 40.8476,
  });

  expect(suggestRoutes([trip(), back], [])).toHaveLength(2);
});

test('the most-taken journey comes first, and only three are offered', () => {
  const other = (n: number, times: number) =>
    Array.from({ length: times }, (_, i) =>
      trip({
        id: `t${n}${i}`,
        booking_id: `b${n}${i}`,
        destination_label: `Προορισμός ${n}`,
        destination_lon: 25 + n / 10,
        destination_lat: 41,
      }),
    );

  const got = suggestRoutes([...other(1, 1), ...other(2, 3), ...other(3, 2), ...other(4, 1)], []);

  expect(got).toHaveLength(3);
  expect(got.map((s) => s.count)).toEqual([3, 2, 1]);
});

// There is no point offering a seat on a journey somebody already has one for.
test('a journey already coming up is not suggested again', () => {
  expect(suggestRoutes([trip()], [upcoming()])).toHaveLength(0);
});

test('a different upcoming trip hides nothing', () => {
  expect(suggestRoutes([trip()], [upcoming({ destination_label: 'Ξάνθη' })])).toHaveLength(1);
});

// A pin needs both ends. Half a journey prefills half a form, which is worse
// than offering nothing.
test.each([
  ['a missing coordinate', { destination_lon: Number.NaN }],
  ['an impossible coordinate', { origin_lat: 999 }],
  ['a blank label', { origin_label: '  ' }],
])('%s drops the suggestion', (_name, over) => {
  expect(suggestRoutes([trip(over as Partial<PastTrip>)], [])).toHaveLength(0);
});

test('no history suggests nothing', () => {
  expect(suggestRoutes([], [])).toEqual([]);
});

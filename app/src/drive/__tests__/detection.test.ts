import * as Location from 'expo-location';

import { detectionWindows, drivingNow } from '../detection';
import type { TripSummary } from '../../api/endpoints';

jest.mock('expo', () => ({ requireOptionalNativeModule: () => null }));
jest.mock('expo-location', () => ({
  getMotionActivityAsync: jest.fn(),
  MotionActivityConfidence: { Low: 0, Medium: 1, High: 2 },
}));

const HOUR = 3_600_000;
const trip = (over: Partial<TripSummary>): TripSummary =>
  ({
    id: 't1',
    role: 'driver',
    status: 'scheduled',
    depart_earliest: '2026-08-04T04:00:00Z',
    depart_latest: '2026-08-04T04:30:00Z',
    ...over,
  }) as TripSummary;

// A car journey means a departure only near the driver's own: an hour either
// side of its window, which is what the server lets an empty car move by.
test("the windows are the driver's own departures, an hour either side", () => {
  expect(
    detectionWindows([
      trip({}),
      trip({ id: 't2', role: 'passenger' }),
      trip({ id: 't3', status: 'cancelled' }),
    ]),
  ).toEqual([[Date.parse('2026-08-04T04:00:00Z') - HOUR, Date.parse('2026-08-04T04:30:00Z') + HOUR, 't1']]);
});

const moving = (automotive: boolean, confidence = 2) => ({
  activities: { automotive: { detected: automotive, confidence } },
  timestamp: 0,
});

test('says which departure the driver seems to be on, when the phone is in a car', async () => {
  (Location.getMotionActivityAsync as jest.Mock).mockResolvedValue(moving(true));
  expect(await drivingNow([trip({})], Date.parse('2026-08-04T03:40:00Z'))).toBe('t1');
});

test('says nothing outside a window, on foot, or on a weak guess', async () => {
  (Location.getMotionActivityAsync as jest.Mock).mockResolvedValue(moving(true));
  expect(await drivingNow([trip({})], Date.parse('2026-08-04T08:00:00Z'))).toBeNull();
  (Location.getMotionActivityAsync as jest.Mock).mockResolvedValue(moving(false));
  expect(await drivingNow([trip({})], Date.parse('2026-08-04T04:10:00Z'))).toBeNull();
  (Location.getMotionActivityAsync as jest.Mock).mockResolvedValue(moving(true, 0));
  expect(await drivingNow([trip({})], Date.parse('2026-08-04T04:10:00Z'))).toBeNull();
});

test('a phone that cannot tell says nothing', async () => {
  (Location.getMotionActivityAsync as jest.Mock).mockRejectedValue(new Error('no permission'));
  expect(await drivingNow([trip({})], Date.parse('2026-08-04T04:10:00Z'))).toBeNull();
});

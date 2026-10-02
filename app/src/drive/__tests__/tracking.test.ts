import * as Location from 'expo-location';

import { api } from '../../api/endpoints';
import { ApiError } from '../../api/client';
import { storage } from '../../state/secureStorage';
import {
  setDriveStops,
  startDriveTracking,
  stopDriveTracking,
  uploadLatest,
  visitedStops,
} from '../tracking';

jest.mock('expo-task-manager', () => ({ defineTask: jest.fn() }));
jest.mock('expo-location', () => ({
  Accuracy: { Balanced: 3 },
  ActivityType: { AutomotiveNavigation: 2 },
  requestForegroundPermissionsAsync: jest.fn(),
  startLocationUpdatesAsync: jest.fn(() => Promise.resolve()),
  stopLocationUpdatesAsync: jest.fn(() => Promise.resolve()),
  hasStartedLocationUpdatesAsync: jest.fn(),
  getLastKnownPositionAsync: jest.fn(),
}));
jest.mock('../../api/endpoints', () => ({
  api: {
    putPosition: jest.fn(() => Promise.resolve()),
    stopPosition: jest.fn(() => Promise.resolve()),
  },
}));
jest.mock('../../state/secureStorage', () => {
  const m = new Map<string, string>();
  return {
    storage: {
      getItemAsync: jest.fn(async (k: string) => m.get(k) ?? null),
      setItemAsync: jest.fn(async (k: string, v: string) => void m.set(k, v)),
      deleteItemAsync: jest.fn(async (k: string) => void m.delete(k)),
    },
  };
});

const fix = (lat: number, ts: number, accuracy = 15) => ({
  coords: { latitude: lat, longitude: 25.9, accuracy },
  timestamp: ts,
});

beforeEach(async () => {
  jest.clearAllMocks();
  await storage.deleteItemAsync('carpool.driveTrip');
});

test('a denied permission starts nothing and says so', async () => {
  (Location.requestForegroundPermissionsAsync as jest.Mock).mockResolvedValue({
    granted: false,
  });
  expect(await startDriveTracking('trip-1', { title: 't', body: 'b' })).toBe('denied');
  expect(Location.startLocationUpdatesAsync).not.toHaveBeenCalled();
});

// Coarse on purpose: a fix every 150 m or half a minute is plenty for
// "arrives in about seven minutes", and the battery is the driver's.
test('starts sharing the car sparingly, with the notification Android requires', async () => {
  (Location.requestForegroundPermissionsAsync as jest.Mock).mockResolvedValue({
    granted: true,
  });
  expect(
    await startDriveTracking('trip-1', {
      title: 'Μοιράζεσαι',
      body: 'τη διαδρομή',
    }),
  ).toBe('started');
  const [, opts] = (Location.startLocationUpdatesAsync as jest.Mock).mock.calls[0];
  expect(opts).toMatchObject({
    accuracy: 3,
    distanceInterval: 150,
    activityType: 2,
    showsBackgroundLocationIndicator: true,
    pausesUpdatesAutomatically: false,
    foregroundService: {
      notificationTitle: 'Μοιράζεσαι',
      notificationBody: 'τη διαδρομή',
    },
  });
});

// The last fix of a batch is the car; the earlier ones are history nobody
// keeps. It goes up with how good it is and when the phone took it.
test('uploads only the newest fix of a batch, with its accuracy and time', async () => {
  await storage.setItemAsync('carpool.driveTrip', 'trip-1');
  await uploadLatest([fix(40.81, 1000), fix(40.83, 3000, 22), fix(40.82, 2000)]);
  expect(api.putPosition).toHaveBeenCalledTimes(1);
  expect(api.putPosition).toHaveBeenCalledWith('trip-1', 40.83, 25.9, {
    accuracy_m: 22,
    observed_at: new Date(3000).toISOString(),
  });
});

test('nothing is uploaded once the drive is no longer being shared', async () => {
  await uploadLatest([fix(40.81, 1000)]);
  expect(api.putPosition).not.toHaveBeenCalled();
});

test('stopping forgets the trip and the position', async () => {
  await storage.setItemAsync('carpool.driveTrip', 'trip-1');
  (Location.hasStartedLocationUpdatesAsync as jest.Mock).mockResolvedValue(true);
  await stopDriveTracking('trip-1');
  expect(Location.stopLocationUpdatesAsync).toHaveBeenCalled();
  expect(await storage.getItemAsync('carpool.driveTrip')).toBeNull();
  expect(api.stopPosition).toHaveBeenCalledWith('trip-1');
});

// «Τέλος» on another phone, or a window that closed because nobody said it:
// the server refuses, and this phone stops sampling.
test('a refusal from the server stops sampling', async () => {
  await storage.setItemAsync('carpool.driveTrip', 'trip-1');
  (api.putPosition as jest.Mock).mockRejectedValueOnce(new ApiError(409, 'not_sharing_now', 'x'));
  (Location.hasStartedLocationUpdatesAsync as jest.Mock).mockResolvedValue(true);
  await uploadLatest([fix(40.81, 1000)]);
  expect(Location.stopLocationUpdatesAsync).toHaveBeenCalled();
  expect(await storage.getItemAsync('carpool.driveTrip')).toBeNull();
});

test('a network failure changes nothing', async () => {
  await storage.setItemAsync('carpool.driveTrip', 'trip-1');
  (api.putPosition as jest.Mock).mockRejectedValueOnce(new TypeError('Network request failed'));
  await uploadLatest([fix(40.81, 1000)]);
  expect(Location.stopLocationUpdatesAsync).not.toHaveBeenCalled();
  expect(await storage.getItemAsync('carpool.driveTrip')).toBe('trip-1');
});

// The task records which stops the car has been at, from every fix of a
// batch, so the hand-off never sends the driver back to one.
test('the fixes record the stops the car has been at', async () => {
  await storage.setItemAsync('carpool.driveTrip', 'trip-1');
  await setDriveStops([
    [25.9, 40.85],
    [25.95, 40.85],
  ]);
  await uploadLatest([
    { coords: { latitude: 40.85, longitude: 25.9003, accuracy: 10 }, timestamp: 1000 },
    { coords: { latitude: 40.85, longitude: 25.93, accuracy: 10 }, timestamp: 2000 },
  ]);
  expect([...(await visitedStops())]).toEqual(['25.90000,40.85000']);
});

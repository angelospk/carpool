// The car's position during a drive, kept going with the screen off.
//
// LiveCard shares a position only while the trip screen is open, which is
// fine for "where is the driver" at a corner and useless for a drive: the
// driver's phone is in a holder showing a maps app. Here the operating system
// delivers fixes to a background task instead — started by the driver
// pressing «Φεύγω», stopped by «Τέλος», never on its own. Only the When In Use
// permission is asked for: a session the driver starts in the foreground
// keeps running in the background, with the blue pill on iOS and the
// ongoing notification Android insists on.
//
// Coarse on purpose. A moving drive needs a fix under two minutes old to time
// a pickup, and "arrives in about seven minutes" needs nothing finer than
// every 150 m or half a minute. These are hints the OS may stretch, which the
// server allows for by refusing to time anything on a stale fix.

import { Platform } from 'react-native';
import * as Location from 'expo-location';
import * as TaskManager from 'expo-task-manager';

import { api } from '../api/endpoints';
import { ApiError } from '../api/client';
import { storage } from '../state/secureStorage';
import { markVisited, type LonLat } from './maps';

export const DRIVE_TASK = 'carpool-drive-position';
const KEY = 'carpool.driveTrip';
const STOPS = 'carpool.driveStops';
const VISITED = 'carpool.driveVisited';

/** The part of a fix that is sent. */
type Fix = {
  coords: Pick<Location.LocationObjectCoords, 'latitude' | 'longitude' | 'accuracy'>;
  timestamp: number;
};

/** Sends the newest fix of a batch for the trip being driven, if there is one. */
export async function uploadLatest(locations: Fix[]): Promise<void> {
  const trip = await storage.getItemAsync(KEY).catch(() => null);
  if (!trip || locations.length === 0) return;
  await noteVisits(locations);
  const last = locations.reduce((a, b) => (b.timestamp > a.timestamp ? b : a));
  try {
    await api.putPosition(trip, last.coords.latitude, last.coords.longitude, {
      accuracy_m: last.coords.accuracy,
      observed_at: new Date(last.timestamp).toISOString(),
    });
  } catch (e) {
    // The server will take no more for this trip: «Τέλος» on another phone,
    // the trip called off, or its window closed because nobody said «Τέλος».
    // Then sampling stops here too, rather than running until the battery
    // notices. A network failure is not that, and changes nothing.
    if (e instanceof ApiError && (e.status === 404 || e.status === 409)) await stopSampling();
  }
}

/**
 * The drive's stops, as the trip last listed them, so the background task can
 * tell which ones the car has been at. Written whenever the trip loads.
 */
export async function setDriveStops(stops: LonLat[]): Promise<void> {
  await storage.setItemAsync(STOPS, JSON.stringify(stops)).catch(() => undefined);
}

/** The stops this drive's fixes have come close to. */
export async function visitedStops(): Promise<Set<string>> {
  const raw = await storage.getItemAsync(VISITED).catch(() => null);
  try {
    return new Set(raw ? (JSON.parse(raw) as string[]) : []);
  } catch {
    return new Set();
  }
}

/** Records the stops any fix of this batch came close to. */
async function noteVisits(locations: Fix[]): Promise<void> {
  const raw = await storage.getItemAsync(STOPS).catch(() => null);
  if (!raw) return;
  let stops: LonLat[];
  try {
    stops = JSON.parse(raw) as LonLat[];
  } catch {
    return;
  }
  const before = await visitedStops();
  const fixes = locations.map((l) => [l.coords.longitude, l.coords.latitude] as LonLat);
  const after = markVisited(fixes, stops, before);
  if (after.size !== before.size) await storage.setItemAsync(VISITED, JSON.stringify([...after]));
}

/** Stops sampling without telling the server: for a sign-out, when there is nobody to tell it as. */
export async function forgetDrive(): Promise<void> {
  await stopSampling();
}

async function stopSampling(): Promise<void> {
  await storage.deleteItemAsync(KEY).catch(() => undefined);
  await storage.deleteItemAsync(STOPS).catch(() => undefined);
  await storage.deleteItemAsync(VISITED).catch(() => undefined);
  if (
    Platform.OS !== 'web' &&
    (await Location.hasStartedLocationUpdatesAsync(DRIVE_TASK).catch(() => false))
  ) {
    await Location.stopLocationUpdatesAsync(DRIVE_TASK).catch(() => undefined);
  }
}

// Defined when this module loads, which _layout makes happen at start: the OS
// may launch the app's JavaScript just to hand it positions.
if (Platform.OS !== 'web') {
  TaskManager.defineTask<{ locations: Location.LocationObject[] }>(DRIVE_TASK, async ({ data, error }) => {
    if (error || !data) return;
    await uploadLatest(data.locations);
  });
}

export async function startDriveTracking(
  tripId: string,
  notice: { title: string; body: string },
): Promise<'started' | 'denied' | 'unavailable'> {
  if (Platform.OS === 'web') return 'unavailable';
  const permission = await Location.requestForegroundPermissionsAsync();
  if (!permission.granted) return 'denied';
  if ((await storage.getItemAsync(KEY).catch(() => null)) !== tripId) {
    await storage.deleteItemAsync(VISITED).catch(() => undefined);
  }
  await storage.setItemAsync(KEY, tripId);
  await Location.startLocationUpdatesAsync(DRIVE_TASK, {
    accuracy: Location.Accuracy.Balanced,
    distanceInterval: 150,
    timeInterval: 30_000,
    deferredUpdatesInterval: 30_000,
    deferredUpdatesDistance: 150,
    activityType: Location.ActivityType.AutomotiveNavigation,
    // Left on, it stops updates at a red light that lasts, which is exactly
    // when a waiting passenger wants to know where the car is.
    pausesUpdatesAutomatically: false,
    showsBackgroundLocationIndicator: true,
    foregroundService: {
      notificationTitle: notice.title,
      notificationBody: notice.body,
      killServiceOnDestroy: true,
    },
  });
  return 'started';
}

export async function stopDriveTracking(tripId: string): Promise<void> {
  await stopSampling();
  await api.stopPosition(tripId).catch(() => undefined);
}

/** Whether this phone is sharing the car for that trip right now. */
export async function trackingTrip(): Promise<string | null> {
  return storage.getItemAsync(KEY).catch(() => null);
}

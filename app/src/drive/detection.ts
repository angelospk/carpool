// Noticing that a driver is in their car, around one of their departures.
//
// Two ways, both opt-in and both on the phone:
//
// - Android, app closed: a small native module (modules/drive-detect) asks
//   Google Play services to say when the phone starts moving in a vehicle,
//   and shows one local notification if that happens inside one of the
//   windows below. The app writes the windows whenever it loads trips.
// - Any phone, app open: the motion coprocessor's "automotive", read once
//   when the home screen shows, inside the same windows.
//
// iOS has no way to wake an app because it started driving; a Shortcuts
// automation on Driving Focus or CarPlay opening carpool://driving is the
// documented tip for that. Nothing here sends a position anywhere. See
// specs/DRIVING-NOW.md, phase 3.

import { Platform } from 'react-native';
import { requireOptionalNativeModule } from 'expo';
import * as Location from 'expo-location';

import type { TripSummary } from '../api/endpoints';
import { storage } from '../state/secureStorage';

type Native = {
  start(title: string, body: string): Promise<boolean>;
  stop(): Promise<void>;
  isOn(): Promise<boolean>;
  setWindows(json: string): Promise<void>;
};
const native = Platform.OS === 'android' ? requireOptionalNativeModule<Native>('DriveDetect') : null;

const KEY = 'carpool.detectDriving';
const HOUR = 3_600_000;

/** [start, end, departure id]: when being in a car means one of the driver's departures. */
export function detectionWindows(trips: TripSummary[]): [number, number, string][] {
  return trips
    .filter((t) => t.role === 'driver' && t.status === 'scheduled')
    .map((t) => [Date.parse(t.depart_earliest) - HOUR, Date.parse(t.depart_latest) + HOUR, t.id]);
}

export async function detectionOn(): Promise<boolean> {
  return (await storage.getItemAsync(KEY).catch(() => null)) === 'on';
}

/**
 * Turns detection on. Asks for the motion permission ("Physical activity" on
 * Android, "Motion & Fitness" on iOS) and, on Android, starts the watch.
 * Returns false when the permission is refused.
 */
export async function enableDetection(notice: { title: string; body: string }): Promise<boolean> {
  if (Platform.OS === 'web') return false;
  const permission = await Location.requestMotionActivityPermissionsAsync().catch(() => null);
  if (!permission?.granted) return false;
  if (native && !(await native.start(notice.title, notice.body).catch(() => false))) return false;
  await storage.setItemAsync(KEY, 'on');
  return true;
}

export async function disableDetection(): Promise<void> {
  await storage.deleteItemAsync(KEY).catch(() => undefined);
  await native?.stop().catch(() => undefined);
}

/** Tells the Android watch which windows count. Harmless elsewhere. */
export async function syncDetectionWindows(trips: TripSummary[]): Promise<void> {
  await native?.setWindows(JSON.stringify(detectionWindows(trips))).catch(() => undefined);
}

/**
 * The departure the driver seems to be setting off on: one whose window holds
 * now, with the phone confidently in a vehicle. Null on any doubt.
 */
export async function drivingNow(trips: TripSummary[], now: number): Promise<string | null> {
  const window = detectionWindows(trips).find(([start, end]) => now >= start && now <= end);
  if (!window) return null;
  try {
    const { activities } = await Location.getMotionActivityAsync();
    const car = activities.automotive;
    return car.detected && car.confidence >= Location.MotionActivityConfidence.Medium ? window[2] : null;
  } catch {
    return null;
  }
}

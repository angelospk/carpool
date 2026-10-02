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
import * as Notifications from 'expo-notifications';

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

/**
 * Whether this account turned detection on, on this phone. The consent is
 * kept with the account it came from: somebody else signing in on the same
 * phone has not agreed to anything.
 */
export async function detectionOn(account: string | undefined): Promise<boolean> {
  if (!account) return false;
  return (await storage.getItemAsync(KEY).catch(() => null)) === account;
}

/**
 * Turns detection on for this account. Asks for the motion permission
 * ("Physical activity" on Android, "Motion & Fitness" on iOS) and for
 * notifications, without which the Android watch has no way to speak, and
 * on Android starts the watch — on only once Play services has accepted it.
 * Returns false when any of that is refused.
 */
export async function enableDetection(
  account: string,
  notice: { title: string; body: string },
): Promise<boolean> {
  if (Platform.OS === 'web') return false;
  const motion = await Location.requestMotionActivityPermissionsAsync().catch(() => null);
  if (!motion?.granted) return false;
  if (native) {
    const current = await Notifications.getPermissionsAsync().catch(() => null);
    const notify = current?.granted
      ? current
      : await Notifications.requestPermissionsAsync().catch(() => null);
    if (!notify?.granted) return false;
    if (!(await native.start(notice.title, notice.body).catch(() => false))) return false;
  }
  await storage.setItemAsync(KEY, account);
  return true;
}

/** Off, and the Android watch's windows forgotten. */
export async function disableDetection(): Promise<void> {
  await storage.deleteItemAsync(KEY).catch(() => undefined);
  await native?.stop().catch(() => undefined);
}

/**
 * Tells the Android watch which windows count — this account's departures,
 * and none when this account has not turned detection on.
 */
export async function syncDetectionWindows(trips: TripSummary[], account: string | undefined): Promise<void> {
  const windows = (await detectionOn(account)) ? detectionWindows(trips) : [];
  await native?.setWindows(JSON.stringify(windows)).catch(() => undefined);
}

/**
 * Sign-out, or another account on this phone: detection that was not this
 * account's is turned off, so nobody inherits it and the watch stops
 * speaking about somebody else's departures.
 */
export async function forgetOtherAccounts(account: string | undefined): Promise<void> {
  const owner = await storage.getItemAsync(KEY).catch(() => null);
  if (owner && owner !== account) await disableDetection();
}

/** The departure whose window holds now, if any. */
export function departureNow(trips: TripSummary[], now: number): string | null {
  return detectionWindows(trips).find(([start, end]) => now >= start && now <= end)?.[2] ?? null;
}

const inCar = (o: Location.MotionActivityObject) =>
  o.activities.automotive.detected &&
  o.activities.automotive.confidence >= Location.MotionActivityConfidence.Medium;

/**
 * The departure the driver seems to be setting off on: one whose window holds
 * now, with the phone confidently in a vehicle. Null on any doubt.
 */
export async function drivingNow(trips: TripSummary[], now: number): Promise<string | null> {
  const trip = departureNow(trips, now);
  if (!trip) return null;
  try {
    return inCar(await Location.getMotionActivityAsync()) ? trip : null;
  } catch {
    return null;
  }
}

/**
 * Keeps watching while the screen is open: a driver standing by the car when
 * the screen first looked is not news, the same driver moving off a minute
 * later is. On every motion update the windows are looked at again, so a
 * window that opens while the screen is up is noticed and one that has closed
 * stops counting. Calls back with the departure, or null when there is none
 * to speak of. Null when no departure is still to come.
 */
export async function watchDriving(
  trips: TripSummary[],
  onChange: (trip: string | null) => void,
  clock: () => number = Date.now,
): Promise<{ remove(): void } | null> {
  if (!detectionWindows(trips).some(([, end]) => end > clock())) return null;
  try {
    return await Location.watchMotionActivityAsync((o) => {
      const trip = departureNow(trips, clock());
      onChange(trip && inCar(o) ? trip : null);
    });
  } catch {
    return null;
  }
}

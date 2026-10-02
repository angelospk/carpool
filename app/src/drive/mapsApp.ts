// Which maps app the driver hands a drive to, kept on the device.

import { Platform } from 'react-native';

import { storage } from '../state/secureStorage';
import { MAPS_APPS, type MapsApp } from './maps';

const KEY = 'carpool.mapsApp';

/** What the phone already has: Apple Maps on an iPhone, Google Maps elsewhere. */
export const defaultMapsApp = (): MapsApp => (Platform.OS === 'ios' ? 'apple' : 'google');

export async function getMapsApp(): Promise<MapsApp> {
  const saved = await storage.getItemAsync(KEY).catch(() => null);
  return saved && (MAPS_APPS as readonly string[]).includes(saved) ? (saved as MapsApp) : defaultMapsApp();
}

export async function setMapsApp(app: MapsApp): Promise<void> {
  await storage.setItemAsync(KEY, app).catch(() => undefined);
}

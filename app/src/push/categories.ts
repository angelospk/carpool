// The buttons on a notification, registered with the phone once.
//
// An offer on a drive is answered from the notification: «Πάρ' τον» opens the
// app on the offer, accepts it and hands the pickup to the driver's maps app;
// «Όχι» says no without opening anything. The server sends the category's name
// (dispatch.DriveOfferCategory) and the two have to agree.

import { Platform } from 'react-native';
import * as Notifications from 'expo-notifications';
import type { TFunction } from 'i18next';

export const DRIVE_OFFER = 'drive_offer';
export const TAKE = 'take';
export const SKIP = 'skip';

export async function registerCategories(t: TFunction): Promise<void> {
  if (Platform.OS === 'web') return;
  await Notifications.setNotificationCategoryAsync(DRIVE_OFFER, [
    { identifier: TAKE, buttonTitle: t('drive.take'), options: { opensAppToForeground: true } },
    { identifier: SKIP, buttonTitle: t('drive.skip'), options: { opensAppToForeground: false } },
  ]).catch(() => undefined);
}

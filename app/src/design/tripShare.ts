// The message a passenger sends somebody they trust before getting in: who,
// in what car, from where to where, and when.
//
// It is the oldest safety feature there is, and it needs no server: "if you
// do not hear from me by nine, this is who I was with". So it is a sentence,
// built here where it can be tested, and handed to the share sheet.
//
// The plate is in it for the passenger, because a plate is the one thing a
// friend can do anything with. A driver sharing their own trip has no reason
// to broadcast their plate, and their passengers' names are the passengers'
// to share.

import type { TripDetail } from '../api/endpoints';
import { athensDay, athensTime } from './format';

type Translate = (key: string, options?: Record<string, unknown>) => string;

export function tripShareMessage(trip: TripDetail, t: Translate): string {
  const when = `${athensDay(trip.depart_earliest)} ${athensTime(trip.depart_earliest)}`;
  const where = `${trip.origin_label.trim()} → ${trip.destination_label.trim()}`;
  const lines = [t('tripShare.headline', { where, when })];

  if (trip.role === 'passenger') {
    const car = [trip.driver.car_model, trip.driver.car_color].filter((x) => x && x.trim()).join(' ');
    lines.push(t('tripShare.driver', { name: trip.driver.name }));
    if (car || trip.driver.car_plate) {
      lines.push([car, trip.driver.car_plate].filter(Boolean).join(' · '));
    }
  } else {
    lines.push(t('tripShare.driving', { count: trip.passengers.length }));
  }
  return lines.join('\n');
}

// Dates and times, always in Athens.
//
// The server sends instants. A traveller looking at their phone abroad, or a
// device sitting on UTC, must still see the local departure time everyone
// agreed on — so the zone is pinned here rather than left to the device.

const ZONE = 'Europe/Athens';
const LOCALE = 'el-GR';

export function athensTime(instant: string): string {
  return new Date(instant).toLocaleTimeString(LOCALE, {
    timeZone: ZONE,
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  });
}

export function athensDay(instant: string): string {
  return new Date(instant).toLocaleDateString(LOCALE, {
    timeZone: ZONE,
    weekday: 'short',
    day: 'numeric',
    month: 'short',
  });
}

/**
 * Whether an item opens a new day heading in a list.
 *
 * Comparison is on the Athens calendar date: a 01:30 departure is the small
 * hours of the *next* Athens day even though it is still the previous day in
 * UTC, and filing it under yesterday would be wrong on screen.
 */
export function startsNewDay(instant: string, previous: string | undefined): boolean {
  if (!previous) return true;
  return calendarDate(instant) !== calendarDate(previous);
}

function calendarDate(instant: string): string {
  // en-CA gives YYYY-MM-DD, which compares as a string.
  return new Date(instant).toLocaleDateString('en-CA', { timeZone: ZONE });
}

/**
 * Money, in the same pinned locale as the dates above.
 *
 * Pinned rather than following the device, for the reason the zone is pinned:
 * a Greek user with an English phone should see the figure the way everybody
 * around them writes it, and a price that changes shape with a system setting
 * is a price people distrust.
 */
export function euros(cents: number): string {
  return new Intl.NumberFormat(LOCALE, {
    style: 'currency',
    currency: 'EUR',
    minimumFractionDigits: 2,
  }).format(cents / 100);
}

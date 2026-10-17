// Dates and times, always in Athens.
//
// The server sends instants. A traveller looking at their phone abroad, or a
// device sitting on UTC, must still see the local departure time everyone
// agreed on — so the zone is pinned here rather than left to the device.

import type { TFunction } from 'i18next';

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

/**
 * Which greeting fits the hour in Athens. Past midnight is neither morning nor
 * evening, so it gets the one that is always right.
 */
export function greetingKey(now: Date): string {
  const hour = Number(
    now.toLocaleTimeString('en-GB', { timeZone: ZONE, hour: '2-digit', hour12: false }),
  ) % 24;
  if (hour < 5) return 'home.greetingNight';
  if (hour < 12) return 'home.greetingMorning';
  return 'home.greetingEvening';
}

/** How far ahead the server plans departures; see jobs.HorizonDays. */
export const HORIZON_DAYS = 21;

/**
 * The day a weekly arrangement asked for now would run until, for the sentence
 * that offers one. The server stores now + 21 days; this says the same thing.
 */
export function repeatUntil(now: Date): string {
  const until = new Date(now.getTime() + HORIZON_DAYS * 24 * 60 * 60 * 1000);
  return until.toLocaleDateString(LOCALE, { timeZone: ZONE, day: 'numeric', month: 'long' });
}

/**
 * A calendar date the server sends as YYYY-MM-DD, named like the trip cards
 * name days. Read as noon UTC so no zone can move it to a neighbouring day.
 */
export function calendarDay(date: string): string {
  const d = new Date(`${date}T12:00:00Z`);
  if (Number.isNaN(d.getTime())) return date;
  return d.toLocaleDateString(LOCALE, { timeZone: 'UTC', weekday: 'short', day: 'numeric', month: 'short' });
}

/** A number with a fixed count of decimals, written the Greek way: 2,37. */
export function decimal(n: number, digits: number): string {
  return new Intl.NumberFormat(LOCALE, {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
    useGrouping: false,
  }).format(n);
}

/** A contribution in euros (not cents), e.g. «2,37 €». */
export function eur(amount: number): string {
  return euros(Math.round(amount * 100));
}

/**
 * What a passenger pays, said so that a booking of several seats is not read as
 * one: a price is per seat, so two seats show each and in all (QA F4).
 */
export function seatPrice(t: TFunction, price: number, seats?: number | null): string {
  if (!seats || seats <= 1) return eur(price);
  return t('home.perSeat', { each: eur(price), total: eur(price * seats) });
}

/** The words that go over a ticket's per-seat fare when it holds several seats. */
export function seatPriceNote(t: TFunction, price: number, seats?: number | null): string | undefined {
  if (!seats || seats <= 1) return undefined;
  return t('home.perSeatTotal', { total: eur(price * seats) });
}

/** A distance to one decimal, e.g. «57,9 χλμ». */
export function km(n: number): string {
  return `${decimal(n, 1)}\u00a0χλμ`;
}

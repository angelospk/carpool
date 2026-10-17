// The clock side of «Απόδραση το Σαββατοκύριακο».
//
// The server counts days as Sunday 0, Friday 5, Saturday 6; the rest of the app
// (weekday labels, publish) counts Monday 1 to Sunday 7. This is the one place
// the two meet. Everything takes `now`, so Thursday 18:00 can be argued with in
// a test rather than by changing the clock on a phone.

import type { WeekendPlan } from '../api/endpoints';
import { athensTime } from './format';
import { athensDate, athensInstant, nextDate } from './when';

/** The days a plan can leave on, in the order they are offered. */
export const WEEKEND_DOWS = [5, 6, 0] as const;

/** The server's day (Sunday 0) as the app's ISO day (Sunday 7). */
export function isoOf(dow: number): number {
  return dow === 0 ? 7 : dow;
}

const ISO_BY_SHORT: Record<string, number> = { Mon: 1, Tue: 2, Wed: 3, Thu: 4, Fri: 5, Sat: 6, Sun: 7 };

/** The Athens weekday of an instant, Monday 1 to Sunday 7. */
export function athensIsoWeekday(at: Date): number {
  const short = new Intl.DateTimeFormat('en-US', { timeZone: 'Europe/Athens', weekday: 'short' }).format(at);
  return ISO_BY_SHORT[short];
}

/** Friday, Saturday or Sunday in Athens. */
export function isWeekendDay(at: Date): boolean {
  return athensIsoWeekday(at) >= 5;
}

function addDays(date: string, n: number): string {
  let out = date;
  for (let i = 0; i < n; i++) out = nextDate(out);
  return out;
}

/**
 * The Friday of the weekend that is coming or under way, as an Athens date. On
 * a Saturday it is this weekend, as for the server. Monday to Wednesday there is
 * no question yet, so null.
 */
export function comingFriday(now: Date): string | null {
  const wd = athensIsoWeekday(now);
  const today = athensDate(now);
  if (wd === 4) return nextDate(today);
  if (wd < 4) return null;
  // Friday is 0 days back, Saturday 1, Sunday 2.
  return new Date(Date.parse(`${today}T00:00:00Z`) - (wd - 5) * 86_400_000).toISOString().slice(0, 10);
}

/**
 * Whether «Φεύγεις αυτό το Σαββατοκύριακο;» is due: from Thursday 18:00 until
 * the outbound window opens, while the weekend is still open. The server sends
 * no flag for it, only `this_week`, so the clock decides,
 * and once the server has an answer for the weekend it is not asked again.
 */
export function isAskedThisWeek(
  plan: Pick<WeekendPlan, 'this_week' | 'depart_dow' | 'depart_time' | 'answered_this_week'>,
  now: Date,
): boolean {
  if (plan.this_week !== 'open' || plan.answered_this_week) return false;
  const friday = comingFriday(now);
  if (!friday) return false;
  if (athensIsoWeekday(now) === 4 && athensTime(now.toISOString()) < '18:00') return false;

  const [h, m] = plan.depart_time.split(':').map(Number);
  const day = addDays(friday, isoOf(plan.depart_dow) - 5);
  const opens = athensInstant(day, h).getTime() + m * 60_000;
  return now.getTime() < opens;
}

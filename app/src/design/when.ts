// When somebody wants to travel, as a window the matcher can use.
//
// The screen used to send "now until two hours from now" without ever asking,
// which quietly turned a commuting app into an on-demand one. The journey this
// exists for is 07:40 on a Tuesday, decided the night before — so the question
// has to be askable, and the answer has to be a window rather than a moment,
// because a driver leaving at 07:35 and one leaving at 07:50 are both the ride
// you meant.
//
// Everything here is pure and takes `now`, so the bands can be argued with in
// a test rather than by changing the clock on a phone.

/** Athens, always — the same pinning `format.ts` does, for the same reason. */
const ZONE = 'Europe/Athens';

/** Today, tomorrow, or a calendar date further on as `YYYY-MM-DD`. */
export type Day = 'today' | 'tomorrow' | `${number}-${number}-${number}`;

/**
 * A band of the day, named the way people say it rather than in hours.
 *
 * `now` is not a band: it is the answer "I am standing here", and it starts at
 * this minute rather than at the top of some hour.
 */
export type Band = 'now' | 'morning' | 'midday' | 'afternoon' | 'evening';

/** Athens wall-clock hours, [from, to). */
const BAND_HOURS: Record<Exclude<Band, 'now'>, [number, number]> = {
  morning: [6, 10],
  midday: [10, 14],
  afternoon: [14, 18],
  evening: [18, 22],
};

/** How long "now" reaches forward. Two hours was the old hardcoded window. */
const NOW_WINDOW_MS = 2 * 60 * 60 * 1000;

export type Window = { start: Date; end: Date };

function wallClock(at: Date, opts: Intl.DateTimeFormatOptions): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone: ZONE, ...opts }).format(at);
}

/** The Athens calendar date of an instant, as `YYYY-MM-DD`. */
export function athensDate(at: Date): string {
  return wallClock(at, { year: 'numeric', month: '2-digit', day: '2-digit' });
}

/** How far Athens is ahead of UTC at a given instant, in minutes. */
function offsetMinutes(at: Date): number {
  // Read the same instant as an Athens wall clock, then treat that reading as
  // if it were UTC: the difference is the offset. Cheaper and more portable
  // than shipping a timezone database.
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: ZONE,
    hour12: false,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).formatToParts(at);
  const get = (t: string) => Number(parts.find((p) => p.type === t)!.value);
  const asUtc = Date.UTC(
    get('year'),
    get('month') - 1,
    get('day'),
    get('hour') % 24,
    get('minute'),
    get('second'),
  );
  return Math.round((asUtc - at.getTime()) / 60000);
}

/**
 * The instant at which the Athens wall clock reads `date` at `hour`.
 *
 * Two passes, because the offset has to be read at the answer rather than at
 * the guess: on the last Sunday in March the guess lands an hour out, and one
 * correction moves it onto the right side of the change.
 */
export function athensInstant(date: string, hour: number): Date {
  let at = new Date(`${date}T${String(hour).padStart(2, '0')}:00:00Z`);
  for (let i = 0; i < 2; i++) {
    at = new Date(new Date(`${date}T${String(hour).padStart(2, '0')}:00:00Z`).getTime() - offsetMinutes(at) * 60000);
  }
  return at;
}

/** The Athens calendar date one day after `date`. */
export function nextDate(date: string): string {
  const [y, m, d] = date.split('-').map(Number);
  const next = new Date(Date.UTC(y, m - 1, d + 1));
  return next.toISOString().slice(0, 10);
}

/** The Athens calendar date a day names. */
export function dateOf(day: Day, now: Date): string {
  if (day === 'today') return athensDate(now);
  if (day === 'tomorrow') return nextDate(athensDate(now));
  return day;
}

/**
 * A picked date the clock has since reached is today. A date picked on Sunday
 * for Tuesday and sent on Tuesday night is otherwise a window that starts at
 * this minute and ends that morning: backwards, and refused by the server.
 */
function settled(day: Day, now: Date): Day {
  return day !== 'today' && day !== 'tomorrow' && day <= athensDate(now) ? 'today' : day;
}

/**
 * Whether a band is still worth offering.
 *
 * A band whose last minute has already passed is not a journey anybody can
 * take, and offering it produces a request the matcher will never fill. Bands
 * on a later day are always open.
 */
export function bandIsOpen(picked: Day, band: Band, now: Date): boolean {
  const day = settled(picked, now);
  if (day !== 'today') return true;
  if (band === 'now') return true;
  const [, to] = BAND_HOURS[band];
  return athensInstant(athensDate(now), to).getTime() > now.getTime();
}

/**
 * The window to ask for.
 *
 * A band that has already started is clamped to this minute: asking to be
 * picked up at 06:00 when it is 08:30 is asking for a ride that has gone.
 *
 * A band whose last minute has also gone means the same band tomorrow. That is
 * the same clamping one step further on — asking for "morning" at eight in the
 * evening means tomorrow morning — and it is the case that used to produce a
 * window running backwards, because the start was clamped to now and the end
 * was not. The result was start 20:00, end 10:00, which the server's own CHECK
 * refuses, so the passenger got an error with nothing in it to read.
 *
 * It is reachable because the selected band lives outside the picker: the chip
 * disappears once bandIsOpen goes false, and the selection does not.
 */
export function departureWindow(picked: Day, band: Band, now: Date): Window {
  const day = settled(picked, now);
  if (band === 'now') return { start: now, end: new Date(now.getTime() + NOW_WINDOW_MS) };

  const today = athensDate(now);
  const gone = day === 'today' && !bandIsOpen(day, band, now);
  const date = day !== 'today' ? dateOf(day, now) : gone ? nextDate(today) : today;
  const [from, to] = BAND_HOURS[band];
  const start = athensInstant(date, from);
  const end = athensInstant(date, to);
  return { start: start.getTime() < now.getTime() ? now : start, end };
}

/**
 * "0745", "745", "7:45" and "07:45" all mean 07:45; anything else is not a time
 * of day. Returns HH:MM, or null.
 */
export function parseTime(text: string): string | null {
  const m = /^(\d{1,2}):?(\d{2})(?::\d{2})?$/.exec(text.trim());
  if (!m) return null;
  const h = Number(m[1]);
  const min = Number(m[2]);
  if (h > 23 || min > 59) return null;
  return `${String(h).padStart(2, '0')}:${m[2]}`;
}

// Which months an admin can pull, as the server names them.
//
// The month that just ended is the one somebody is paid for, so it comes
// first; the current one is there to check the numbers before the month ends.
// Worked out in Athens, because that is what a trip's date is — the first
// hours of the 1st in Greece are still the previous month in UTC.

const athensMonth = new Intl.DateTimeFormat('en-CA', {
  timeZone: 'Europe/Athens',
  year: 'numeric',
  month: '2-digit',
});

/** YYYY-MM for the Athens month `now` falls in. */
export function monthOf(now: Date): string {
  const parts = athensMonth.formatToParts(now);
  const year = parts.find((p) => p.type === 'year')?.value;
  const month = parts.find((p) => p.type === 'month')?.value;
  return `${year}-${month}`;
}

/** The month before a YYYY-MM month. */
export function previousMonth(month: string): string {
  const [y, m] = month.split('-').map(Number);
  return m === 1 ? `${y - 1}-12` : `${y}-${String(m - 1).padStart(2, '0')}`;
}

/** Last month, then this month. */
export function exportMonths(now: Date): [string, string] {
  const current = monthOf(now);
  return [previousMonth(current), current];
}

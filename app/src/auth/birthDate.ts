/**
 * Reading a date of birth the way a Greek person types one.
 *
 * The server takes YYYY-MM-DD, which is nobody's handwriting. Here it is typed
 * ΗΗ/ΜΜ/ΕΕΕΕ and converted, so the format the API wants stays the API's
 * business.
 *
 * Everything in here is a second opinion. The server decides who gets in; this
 * exists so that somebody plainly too young is told before a round trip instead
 * of after one, and so the button is not live over a half-typed date.
 */

/** ADULT_YEARS matches internal/domain/age.go. Eighteen, as Greek law has it. */
export const ADULT_YEARS = 18;

/**
 * Turns 01/01/1990 into 1990-01-01, or null if that is not a date.
 *
 * Strict about the shape and about the calendar both: 31/02/1990 parses as a
 * pair of numbers and is still not a day, so the round trip through Date is
 * checked rather than trusted.
 */
export function toISODate(typed: string): string | null {
  const m = /^(\d{2})\/(\d{2})\/(\d{4})$/.exec(typed.trim());
  if (!m) return null;

  const [, dd, mm, yyyy] = m;
  const day = Number(dd);
  const month = Number(mm);
  const year = Number(yyyy);

  // Built in UTC on purpose. A local-time Date shifts the day for anybody east
  // or west of the machine, which turns "born on the 1st" into "born on the
  // 31st" depending on where the phone is.
  const d = new Date(Date.UTC(year, month - 1, day));
  if (
    d.getUTCFullYear() !== year ||
    d.getUTCMonth() !== month - 1 ||
    d.getUTCDate() !== day
  ) {
    return null; // 31 February and friends
  }
  return `${yyyy}-${mm}-${dd}`;
}

/**
 * Whether somebody born on an ISO date has completed ADULT_YEARS by a given
 * day.
 *
 * The birthday itself counts, and a leap-day birth falls on 1 March in a common
 * year — both the same as internal/domain/age.go, because two answers to one
 * question is worse than either answer.
 */
export function isAdult(iso: string, today: Date = new Date()): boolean {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
  if (!m) return false;
  const [, yyyy, mm, dd] = m;

  const eighteenth = new Date(
    Date.UTC(Number(yyyy) + ADULT_YEARS, Number(mm) - 1, Number(dd)),
  );
  // 29 February plus eighteen years is 1 March in a common year, which is what
  // Date.UTC does when it rolls the day over — the later of the two candidate
  // days, and so never a day early.

  const now = Date.UTC(today.getFullYear(), today.getMonth(), today.getDate());
  return now >= eighteenth.getTime();
}

/**
 * Puts the slashes in while somebody types, so the field can use the number pad.
 *
 * A number pad has no slash key, and a full keyboard for a date is four extra
 * taps and a chance to type a letter. Digits go in, ΗΗ/ΜΜ/ΕΕΕΕ comes out, and
 * deleting works because the separators are derived from the digits rather than
 * kept in the text.
 */
export function formatTypedDate(input: string): string {
  const digits = input.replace(/\D/g, '').slice(0, 8);
  const parts = [digits.slice(0, 2), digits.slice(2, 4), digits.slice(4, 8)];
  return parts.filter((p) => p.length > 0).join('/');
}

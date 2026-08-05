import { toISODate, isAdult, formatTypedDate } from '../birthDate';

describe('reading a typed date of birth', () => {
  it('turns the Greek way of writing a date into the way the server reads one', () => {
    expect(toISODate('01/01/1990')).toBe('1990-01-01');
    expect(toISODate(' 15/03/2008 ')).toBe('2008-03-15');
  });

  it('refuses anything that is not a whole date', () => {
    for (const typed of ['', '1/1/1990', '01/01', '1990-01-01', '01/01/90', 'χθες']) {
      expect(toISODate(typed)).toBeNull();
    }
  });

  // A pair of numbers in the right shape is not the same as a day. 31 February
  // rolls over into March if you let it, which would store a date the person
  // did not type.
  it('refuses a day that is not on the calendar', () => {
    expect(toISODate('31/02/1990')).toBeNull();
    expect(toISODate('31/04/1990')).toBeNull();
    expect(toISODate('29/02/2007')).toBeNull(); // 2007 was not a leap year
    expect(toISODate('29/02/2008')).toBe('2008-02-29');
  });
});

describe('the eighteen rule', () => {
  const on = (iso: string) => new Date(`${iso}T12:00:00Z`);

  it('counts the birthday itself', () => {
    expect(isAdult('2008-03-15', on('2026-03-14'))).toBe(false);
    expect(isAdult('2008-03-15', on('2026-03-15'))).toBe(true);
    expect(isAdult('2008-03-15', on('2026-03-16'))).toBe(true);
  });

  // The same answer the server gives, which is the whole point of it being
  // written down twice.
  it('puts a leap-day eighteenth birthday on the first of March', () => {
    expect(isAdult('2008-02-29', on('2026-02-28'))).toBe(false);
    expect(isAdult('2008-02-29', on('2026-03-01'))).toBe(true);
  });

  it('says no to anything it cannot read, rather than yes', () => {
    for (const iso of ['', 'nonsense', '15/03/2008']) {
      expect(isAdult(iso, on('2026-08-05'))).toBe(false);
    }
  });
});

describe('typing a date on a number pad', () => {
  it('puts the slashes in as the digits arrive', () => {
    expect(formatTypedDate('0')).toBe('0');
    expect(formatTypedDate('01')).toBe('01');
    expect(formatTypedDate('011')).toBe('01/1');
    expect(formatTypedDate('0101')).toBe('01/01');
    expect(formatTypedDate('01011990')).toBe('01/01/1990');
  });

  // The separators come out of the digits, so backspacing over one deletes the
  // digit before it rather than putting the slash straight back.
  it('lets a backspace actually delete something', () => {
    expect(formatTypedDate('01/01/199')).toBe('01/01/199');
    expect(formatTypedDate('01/0')).toBe('01/0');
    expect(formatTypedDate('01/')).toBe('01');
  });

  it('ignores anything that is not a digit, and stops at eight', () => {
    expect(formatTypedDate('01a01b1990')).toBe('01/01/1990');
    expect(formatTypedDate('010119901234')).toBe('01/01/1990');
  });
});

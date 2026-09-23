import { exportMonths, monthName, monthOf, previousMonth } from './months';

describe('months an admin can export', () => {
  it('reads the month in Athens, where the first hours of the 1st are not UTC', () => {
    // 1 August 2026, 00:30 in Athens, and still 31 July in UTC.
    expect(monthOf(new Date('2026-07-31T21:30:00Z'))).toBe('2026-08');
  });

  it('goes back across a year', () => {
    expect(previousMonth('2026-01')).toBe('2025-12');
    expect(previousMonth('2026-10')).toBe('2026-09');
  });

  it('offers last month first, because that is the one somebody is paid for', () => {
    expect(exportMonths(new Date('2026-09-23T09:00:00Z'))).toEqual(['2026-08', '2026-09']);
  });
});

describe('monthName', () => {
  it('names the month rather than printing 2026-08', () => {
    expect(monthName('2026-08')).toMatch(/Αύγουστος 2026/i);
  });
});

import { athensDate, athensInstant, bandIsOpen, departureWindow, nextDate } from './when';

// 14 July 2026, 08:30 in Athens (UTC+3 in summer).
const summerMorning = new Date('2026-07-14T05:30:00Z');
// 14 January 2026, 08:30 in Athens (UTC+2 in winter).
const winterMorning = new Date('2026-01-14T06:30:00Z');

function athensClock(at: Date): string {
  return new Intl.DateTimeFormat('en-GB', {
    timeZone: 'Europe/Athens',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(at);
}

describe('athensDate', () => {
  it('reads the Athens day, not the UTC one', () => {
    // 22:30 UTC on the 14th is 01:30 on the 15th in Athens.
    expect(athensDate(new Date('2026-07-14T22:30:00Z'))).toBe('2026-07-15');
    expect(athensDate(summerMorning)).toBe('2026-07-14');
  });
});

describe('athensInstant', () => {
  it('lands on the wall-clock hour in summer and in winter', () => {
    expect(athensClock(athensInstant('2026-07-14', 6))).toBe('06:00');
    expect(athensClock(athensInstant('2026-01-14', 6))).toBe('06:00');
  });

  it('lands on the hour on the days the clocks change', () => {
    // Athens springs forward at 03:00 on 29 March 2026 and back on 25 October.
    expect(athensClock(athensInstant('2026-03-29', 6))).toBe('06:00');
    expect(athensClock(athensInstant('2026-03-29', 10))).toBe('10:00');
    expect(athensClock(athensInstant('2026-10-25', 6))).toBe('06:00');
    expect(athensClock(athensInstant('2026-10-25', 18))).toBe('18:00');
  });
});

describe('nextDate', () => {
  it('steps a calendar day, across months and years', () => {
    expect(nextDate('2026-07-14')).toBe('2026-07-15');
    expect(nextDate('2026-02-28')).toBe('2026-03-01');
    expect(nextDate('2028-02-28')).toBe('2028-02-29');
    expect(nextDate('2026-12-31')).toBe('2027-01-01');
  });
});

describe('departureWindow', () => {
  it('"now" is this minute plus two hours, unchanged from before', () => {
    const w = departureWindow('today', 'now', summerMorning);
    expect(w.start).toEqual(summerMorning);
    expect(w.end.getTime() - w.start.getTime()).toBe(2 * 60 * 60 * 1000);
  });

  it('a band later today starts at the band, not at now', () => {
    const w = departureWindow('today', 'afternoon', summerMorning);
    expect(athensClock(w.start)).toBe('14:00');
    expect(athensClock(w.end)).toBe('18:00');
  });

  it('a band already under way starts now: the 06:00 ride has gone', () => {
    const w = departureWindow('today', 'morning', summerMorning); // 08:30
    expect(w.start).toEqual(summerMorning);
    expect(athensClock(w.end)).toBe('10:00');
  });

  it('tomorrow is the whole band, whatever time it is today', () => {
    const w = departureWindow('tomorrow', 'morning', summerMorning);
    expect(athensDate(w.start)).toBe('2026-07-15');
    expect(athensClock(w.start)).toBe('06:00');
    expect(athensClock(w.end)).toBe('10:00');
  });

  it('works in winter too, when Athens is an hour closer to UTC', () => {
    const w = departureWindow('tomorrow', 'evening', winterMorning);
    expect(athensDate(w.start)).toBe('2026-01-15');
    expect(athensClock(w.start)).toBe('18:00');
    expect(athensClock(w.end)).toBe('22:00');
  });

  it('rolls into the next month when today is the last of it', () => {
    // 31 July 2026, 23:00 Athens.
    const lateNight = new Date('2026-07-31T20:00:00Z');
    const w = departureWindow('tomorrow', 'morning', lateNight);
    expect(athensDate(w.start)).toBe('2026-08-01');
  });
});

describe('bandIsOpen', () => {
  it('closes a band today once its last minute has passed', () => {
    expect(bandIsOpen('today', 'morning', summerMorning)).toBe(true); // 08:30, ends 10:00
    const afternoon = new Date('2026-07-14T15:30:00Z'); // 18:30 Athens
    expect(bandIsOpen('today', 'morning', afternoon)).toBe(false);
    expect(bandIsOpen('today', 'afternoon', afternoon)).toBe(false);
    expect(bandIsOpen('today', 'evening', afternoon)).toBe(true);
  });

  it('never closes a band tomorrow, and never closes "now"', () => {
    const nearMidnight = new Date('2026-07-14T21:30:00Z'); // 00:30 on the 15th
    expect(bandIsOpen('tomorrow', 'morning', nearMidnight)).toBe(true);
    expect(bandIsOpen('today', 'now', nearMidnight)).toBe(true);
  });
});

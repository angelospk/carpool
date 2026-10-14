import { athensDay, athensTime, calendarDay, decimal, eur, greetingKey, km, repeatUntil, startsNewDay } from './format';

// Every time in this app is an instant from the server rendered in Athens. A
// device left on UTC must still show the departure the driver agreed to.
test('renders instants in Athens, not in the device zone', () => {
  // 04:30 UTC in July is 07:30 in Athens (EEST, +3).
  expect(athensTime('2026-07-29T04:30:00Z')).toBe('07:30');
});

test('holds up across the DST boundary', () => {
  // 04:30 UTC in January is 06:30 in Athens (EET, +2).
  expect(athensTime('2026-01-29T04:30:00Z')).toBe('06:30');
});

test('the day label names the weekday and the date', () => {
  const label = athensDay('2026-07-29T04:30:00Z');

  expect(label).toMatch(/29/);
  expect(label.length).toBeGreaterThan(4);
});

// A late-evening departure in Athens is the *next* day in UTC. Grouping on the
// raw timestamp would file it under the wrong heading.
test('groups by the Athens calendar day, not the UTC one', () => {
  const lateTuesday = '2026-07-28T22:30:00Z'; // 01:30 Wednesday in Athens
  const earlyWednesday = '2026-07-29T04:30:00Z';

  expect(startsNewDay(earlyWednesday, lateTuesday)).toBe(false);
  expect(startsNewDay(lateTuesday, '2026-07-28T10:00:00Z')).toBe(true);
});

test('the first item always starts a day', () => {
  expect(startsNewDay('2026-07-29T04:30:00Z', undefined)).toBe(true);
});

// The greeting is by the Athens clock. It used to be a fixed «Καλημέρα», which
// is what somebody opening the app at half past midnight was told.
test('greets by the time of day in Athens', () => {
  // All July, so Athens is UTC+3.
  expect(greetingKey(new Date('2026-07-29T21:27:00Z'))).toBe('home.greetingNight'); // 00:27
  expect(greetingKey(new Date('2026-07-29T01:59:00Z'))).toBe('home.greetingNight'); // 04:59
  expect(greetingKey(new Date('2026-07-29T02:00:00Z'))).toBe('home.greetingMorning'); // 05:00
  expect(greetingKey(new Date('2026-07-29T08:59:00Z'))).toBe('home.greetingMorning'); // 11:59
  expect(greetingKey(new Date('2026-07-29T09:00:00Z'))).toBe('home.greetingEvening'); // 12:00
  expect(greetingKey(new Date('2026-07-29T20:59:00Z'))).toBe('home.greetingEvening'); // 23:59
});

// "Κρατάει τη θέση σου ως τις" used to end there: the date was passed as ''.
// An arrangement reaches as far as the server plans departures, 21 days.
test('says the day a new weekly arrangement would reach', () => {
  const label = repeatUntil(new Date('2026-07-29T09:00:00Z'));
  expect(label).toMatch(/19/);
  expect(label.length).toBeGreaterThan(4);
});

// A one-off route's date is a calendar date, and a driver reads «Παρ 2 Οκτ»
// faster than «2026-10-02».
test('names a calendar date the way the trip cards do', () => {
  const label = calendarDay('2026-10-02');
  expect(label).toMatch(/2/);
  expect(label).not.toContain('2026-10-02');
  expect(calendarDay('not a date')).toBe('not a date');
});

describe('figures written the Greek way', () => {
  // A decimal point reads as a thousands separator to somebody who writes
  // «2,37 €», and the app was showing both shapes on neighbouring screens.
  it('writes a contribution with a decimal comma', () => {
    expect(eur(2.37)).toBe('2,37\u00a0€');
    expect(eur(3.5)).toBe('3,50\u00a0€');
  });

  it('writes a distance to one decimal with a decimal comma', () => {
    expect(km(57.94)).toBe('57,9\u00a0χλμ');
    expect(km(2.13)).toBe('2,1\u00a0χλμ');
  });

  it('writes a plain decimal with as many places as asked', () => {
    expect(decimal(6.25, 1)).toBe('6,3');
    expect(decimal(1.8589, 3)).toBe('1,859');
    expect(decimal(58, 0)).toBe('58');
  });
});

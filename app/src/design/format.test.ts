import { athensDay, athensTime, startsNewDay } from './format';

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

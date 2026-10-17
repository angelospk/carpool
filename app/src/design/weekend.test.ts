import { athensIsoWeekday, comingFriday, isAskedThisWeek, isoOf, isWeekendDay } from './weekend';

const plan = { this_week: 'open' as const, depart_dow: 5, depart_time: '15:00', answered_this_week: false };

// 2026-10-15 is a Thursday (Athens is UTC+3 in October).
test('the server counts Sunday as 0 and the app as 7', () => {
  expect(isoOf(0)).toBe(7);
  expect(isoOf(5)).toBe(5);
});

test('the weekday is the Athens one, not the device one', () => {
  expect(athensIsoWeekday(new Date('2026-10-15T21:30:00Z'))).toBe(5); // Friday 00:30 in Athens
  expect(isWeekendDay(new Date('2026-10-15T21:30:00Z'))).toBe(true);
  expect(isWeekendDay(new Date('2026-10-15T20:30:00Z'))).toBe(false);
});

test('the coming weekend starts on the Thursday and is this one on a Saturday', () => {
  expect(comingFriday(new Date('2026-10-14T10:00:00Z'))).toBeNull(); // Wednesday
  expect(comingFriday(new Date('2026-10-15T10:00:00Z'))).toBe('2026-10-16');
  expect(comingFriday(new Date('2026-10-17T10:00:00Z'))).toBe('2026-10-16');
  expect(comingFriday(new Date('2026-10-18T10:00:00Z'))).toBe('2026-10-16');
});

test('the Thursday question opens at 18:00 Athens', () => {
  expect(isAskedThisWeek(plan, new Date('2026-10-15T14:59:00Z'))).toBe(false); // 17:59
  expect(isAskedThisWeek(plan, new Date('2026-10-15T15:00:00Z'))).toBe(true); // 18:00
});

test('the question lasts until the outbound window opens', () => {
  expect(isAskedThisWeek(plan, new Date('2026-10-16T11:59:00Z'))).toBe(true); // Friday 14:59
  expect(isAskedThisWeek(plan, new Date('2026-10-16T12:00:00Z'))).toBe(false); // Friday 15:00
  // A Saturday plan is still asked on the Friday.
  expect(isAskedThisWeek({ ...plan, depart_dow: 6 }, new Date('2026-10-16T20:00:00Z'))).toBe(true);
  expect(isAskedThisWeek({ ...plan, depart_dow: 0, depart_time: '10:00' }, new Date('2026-10-17T20:00:00Z'))).toBe(true);
});

test('a weekend already answered is not asked again', () => {
  expect(isAskedThisWeek({ ...plan, answered_this_week: true }, new Date('2026-10-15T16:00:00Z'))).toBe(false);
});

test('only a plan that is still looking is asked', () => {
  const thursday = new Date('2026-10-15T16:00:00Z');
  for (const this_week of ['none', 'matched', 'cancelled'] as const) {
    expect(isAskedThisWeek({ ...plan, this_week }, thursday)).toBe(false);
  }
});

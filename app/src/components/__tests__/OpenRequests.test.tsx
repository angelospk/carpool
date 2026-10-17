import i18n from '../../i18n';
import el from '../../i18n/el.json';
import en from '../../i18n/en.json';

import { daysLine, journeyGroups, leadOf, requestProgress } from '../OpenRequests';
import type { OpenRequest } from '../../api/endpoints';

// requestProgress is a pure function, but it lives beside the card that draws
// it and the module reaches expo-router, which pulls in an untransformed ESM
// dependency. Every screen test in this repo stubs the router for the same
// reason.
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
}));

const t = i18n.getFixedT('el') as unknown as Parameters<typeof requestProgress>[1];

const waiting = (over: Partial<OpenRequest> = {}): OpenRequest =>
  ({
    id: 'q1',
    origin_label: 'Αλεξανδρούπολη',
    destination_label: 'Ορεστιάδα',
    window_start: '2026-09-17T04:30:00Z',
    window_end: '2026-09-17T06:30:00Z',
    seats: 1,
    status: 'open',
    drivers_asked: 0,
    drivers_declined: 0,
    asking_now: false,
    drivers_on_corridor: 1,
    created_at: '2026-09-16T06:00:00Z',
    ...over,
  }) as OpenRequest;

// The gap this closes. "We are asking drivers" and "there is nobody on this
// road to ask" looked identical from the passenger's side, for ever, and the
// request aged out without anybody ever being told which one it was.
test('nobody driving the road is its own answer, not the asking one', () => {
  const nobody = requestProgress(waiting({ drivers_on_corridor: 0 }), t);

  expect(nobody).toBe(el.home.requestNoDrivers);
  expect(nobody).not.toBe(el.home.requestNobody);
});

// The request is not lost while it waits: the server asks as soon as somebody
// publishes that road. Saying so is the difference between waiting and being
// abandoned, and it is why there is no "notify me" button to press — it is
// already what happens.
test('the no-drivers sentence says the request stays open', () => {
  expect(el.home.requestNoDrivers).toMatch(/ανοιχτ|μόλις/i);
});

// It shows wherever push is off (every web build, every build today), so it
// must not promise a notification it cannot send.
test('the no-drivers sentence promises no notification', () => {
  expect(el.home.requestNoDrivers).not.toMatch(/ειδοποι/i);
  expect(en.home.requestNoDrivers).not.toMatch(/notif|told/i);
});

// Somebody does drive it, and nobody has been asked yet. That is a different
// sentence and it was the only one this state ever had.
test('a road with drivers on it and nobody asked yet still reads as not asked yet', () => {
  expect(requestProgress(waiting({ drivers_on_corridor: 2 }), t)).toBe(el.home.requestNobody);
});

// Once an ask is out, how many drivers exist on the corridor stops being the
// news: something is actually happening.
test('an ask in flight outranks the count of drivers on the road', () => {
  const asking = requestProgress(
    waiting({ drivers_asked: 1, asking_now: true, drivers_on_corridor: 0 }),
    t,
  );

  expect(asking).toContain(el.home.requestAsking);
});

// Four seats asked for, and no car on the road has four. Nobody will ever be
// asked, and "nobody asked yet" hid why (QA 2026-10-16, U13).
test('more seats than any car on the road has says so', () => {
  const got = requestProgress(waiting({ seats: 4, drivers_on_corridor: 2, most_seats_on_corridor: 3 }), t);
  expect(got).toBe(t('home.requestTooManySeats', { count: 3 }));
  expect(got).toContain('3');
});

test('seats that fit are not a reason', () => {
  expect(requestProgress(waiting({ seats: 3, drivers_on_corridor: 2, most_seats_on_corridor: 3 }), t)).toBe(
    el.home.requestNobody,
  );
});

// A car passes and its driver does not take the detour. The passenger heard
// only "nobody asked yet" (QA 2026-10-16, U3). No name: nobody agreed to
// anything.
test('a missed detour says by how much, in minutes', () => {
  const got = requestProgress(
    waiting({ drivers_on_corridor: 1, missed: { reason: 'detour_min', need: 11.2, limit: 10 } }),
    t,
  );
  expect(got).toBe(t('home.missedDetourMin', { need: 12, limit: 10 }));
});

test('a missed detour in kilometres, and a driver who takes no stops', () => {
  expect(
    requestProgress(waiting({ drivers_on_corridor: 1, missed: { reason: 'detour_km', need: 6.3, limit: 5 } }), t),
  ).toBe(t('home.missedDetourKm', { need: 7, limit: 5 }));
  expect(
    requestProgress(waiting({ drivers_on_corridor: 1, missed: { reason: 'no_stops', need: null, limit: null } }), t),
  ).toBe(el.home.missedNoStops);
});

// QA 2026-10-17 T F3, U F5: the other reasons a car that passes was not asked.
test('a full car, a car with two groups, and a boot that is full each say so', () => {
  const r = (reason: 'full' | 'party_cap' | 'luggage') =>
    requestProgress(waiting({ drivers_on_corridor: 1, missed: { reason, need: null, limit: null } }), t);
  expect(r('full')).toBe(el.home.missedFull);
  expect(r('party_cap')).toBe(el.home.missedPartyCap);
  expect(r('luggage')).toBe(el.home.missedLuggage);
});

// The road passes 8 km away, outside the driver's radius: the corridor count
// is zero, and the passenger used to be told nobody drives this road.
test('a road that passes too far away says how far, not that nobody drives it', () => {
  const got = requestProgress(
    waiting({ drivers_on_corridor: 0, missed: { reason: 'off_corridor', need: 8.1, limit: 2 } }),
    t,
  );
  expect(got).toBe(t('home.missedOffCorridor', { need: 8 }));
});

test('nobody leaving then says when the nearest one does', () => {
  const got = requestProgress(
    waiting({ drivers_on_corridor: 1, drivers_departing: 0, nearest_departure: '2026-09-17T08:30:00Z' }),
    t,
  );
  expect(got).toBe(t('home.requestNearest', { time: '11:30' }));
});

test('nobody leaving that day at all says so', () => {
  expect(requestProgress(waiting({ drivers_on_corridor: 1, drivers_departing: 0 }), t)).toBe(
    el.home.requestNotThen,
  );
});

test('an older server that does not count departures keeps the old sentence', () => {
  expect(requestProgress(waiting({ drivers_on_corridor: 1 }), t)).toBe(el.home.requestNobody);
});

// The ask lapses after half an hour, and the passenger could not see when.
// They waited without knowing for how long (QA 2026-10-16, U6).
test('a live ask says until when the driver has to answer', () => {
  const asking = requestProgress(
    waiting({ drivers_asked: 1, asking_now: true, ask_expires_at: '2026-07-29T07:42:00Z' }),
    t,
  );

  expect(asking).toContain(el.home.requestAsking);
  expect(asking).toContain('10:42');
});

// An older app, or a server that has not been redeployed, sends no count at
// all. Absent is not zero: claiming "there is nobody" because a field is
// missing is the one wrong answer here.
test('a missing count is not read as an empty road', () => {
  const unknown = requestProgress(waiting({ drivers_on_corridor: undefined }), t);

  expect(unknown).toBe(el.home.requestNobody);
  expect(unknown).not.toBe(el.home.requestNoDrivers);
});

// The count of people who could not still explains why this is taking a while.
test('drivers who declined are still reported alongside the search', () => {
  const searching = requestProgress(
    waiting({ drivers_asked: 2, drivers_declined: 2, drivers_on_corridor: 3 }),
    t,
  );

  expect(searching).toContain(el.home.requestSearching);
  expect(searching).toMatch(/2/);
});

// Every driver on the road has said no or let the ask lapse. "Looking for the
// next driver" promised somebody who does not exist (QA 2026-10-16, U5).
test('when every driver on the road has passed, it says so instead of searching', () => {
  const done = requestProgress(
    waiting({ drivers_asked: 1, drivers_declined: 1, drivers_on_corridor: 1, drivers_untried: 0 }),
    t,
  );

  expect(done).toBe(el.home.requestExhausted);
  expect(done).not.toContain(el.home.requestSearching);
  expect(el.home.requestExhausted).toMatch(/ανοιχτ/);
});

// An ask that lapsed is not a refusal: the driver may never have seen it.
test('the count of drivers who passed does not claim they refused', () => {
  expect(el.home.requestDeclined_one).toMatch(/δεν απάντησε/);
  expect(el.home.requestDeclined_one).not.toMatch(/δεν μπορούσε/);
});

// Absent is not zero here either: without a count, keep searching.
test('a missing count never reads as everybody having passed', () => {
  const unknown = requestProgress(
    waiting({ drivers_asked: 1, drivers_declined: 1, drivers_untried: undefined }),
    t,
  );

  expect(unknown).toContain(el.home.requestSearching);
});

// Nobody has passed yet: an ask that is out and unanswered is waiting.
test('an unanswered ask on an emptied road is still waiting, not exhausted', () => {
  expect(
    requestProgress(
      waiting({ drivers_asked: 1, drivers_declined: 0, drivers_on_corridor: 0, drivers_untried: 0 }),
      t,
    ),
  ).toBe(el.home.requestWaiting);
});

// One driver passed and retired, a new one publishes: the declined count equals
// the corridor, but somebody is still there to ask (Codex review).
test('a new driver after one who passed is still somebody to look for', () => {
  const line = requestProgress(
    waiting({ drivers_asked: 1, drivers_declined: 1, drivers_on_corridor: 1, drivers_untried: 1 }),
    t,
  );

  expect(line).toContain(el.home.requestSearching);
});

// QA 2026-10-17 U F6: the days of one journey were two cards with the same
// sentence, the newest first. They are one card, saying the days.
test('the days of one journey are one entry, earliest day first', () => {
  const groups = journeyGroups([
    waiting({ id: 'b', window_start: '2026-10-25T05:00:00Z', alternative_of: 'a' }),
    waiting({ id: 'a', window_start: '2026-10-18T05:00:00Z' }),
    waiting({ id: 'c', window_start: '2026-10-19T05:00:00Z' }),
  ]);
  expect(groups.map((g) => g.map((r) => r.id))).toEqual([['a', 'b'], ['c']]);
  expect(daysLine(groups[0])).toBe('Κυρ 18 Οκτ ή Κυρ 25 Οκτ');
});

test('the entry speaks for the day being asked about now', () => {
  const [group] = journeyGroups([
    waiting({ id: 'a', window_start: '2026-10-18T05:00:00Z' }),
    waiting({ id: 'b', window_start: '2026-10-25T05:00:00Z', alternative_of: 'a', asking_now: true }),
  ]);
  expect(leadOf(group).id).toBe('b');
});

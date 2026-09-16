import i18n from '../../i18n';
import el from '../../i18n/el.json';

import { requestProgress } from '../OpenRequests';
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
  expect(el.home.requestNoDrivers).toMatch(/ανοιχτ|μόλις|ειδοποι/i);
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
    waiting({ drivers_asked: 2, drivers_declined: 2, drivers_on_corridor: 2 }),
    t,
  );

  expect(searching).toContain(el.home.requestSearching);
  expect(searching).toMatch(/2/);
});

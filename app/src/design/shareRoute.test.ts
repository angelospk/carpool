import i18n from '../i18n';
import el from '../i18n/el.json';
import en from '../i18n/en.json';

import { shareRouteMessage, type ShareableRoute } from './shareRoute';

const t = i18n.getFixedT('el');

const weekdayRoute: ShareableRoute = {
  origin_label: 'Αλεξανδρούπολη',
  destination_label: 'Σουφλί',
  kind: 'recurring',
  weekdays: [1, 2, 3, 4, 5],
  depart_earliest: '07:30',
  seats: 2,
};

// The sentence a driver pastes into the village's Viber group. It has to say
// the four things somebody decides on without opening anything: where from,
// where to, when, and whether there is room.
test('a weekday route reads as a route, a schedule with its time, and the seats', () => {
  const message = shareRouteMessage(weekdayRoute, t);

  expect(message).toContain('Αλεξανδρούπολη → Σουφλί');
  // The schedule and the time are one thought, not two separated items.
  expect(message).toContain('καθημερινές 07:30');
  expect(message).toMatch(/2 θέσεις/);
});

// A route with nothing asking for a reply is a fact nobody acts on. There is no
// store listing and no link to send, so the reply channel is the chat the
// message was pasted into.
test('the message says what to do about it', () => {
  expect(shareRouteMessage(weekdayRoute, t)).toContain(el.share.callToAction);
});

// Five taps compressed into one word. The preset is a set, so a driver whose
// days arrived in another order is still commuting on weekdays.
test('the weekday preset is recognised whatever order the days arrive in', () => {
  expect(shareRouteMessage({ ...weekdayRoute, weekdays: [5, 4, 3, 2, 1] }, t)).toContain(
    'καθημερινές',
  );
});

// A day list that repeats itself is still the same set of days.
test('a repeated day does not stop a preset being a preset', () => {
  expect(shareRouteMessage({ ...weekdayRoute, weekdays: [1, 1, 2, 3, 4, 5] }, t)).toContain(
    'καθημερινές',
  );
});

test('the weekend and every-day presets are named too', () => {
  expect(shareRouteMessage({ ...weekdayRoute, weekdays: [6, 7] }, t)).toContain(el.share.weekend);
  expect(shareRouteMessage({ ...weekdayRoute, weekdays: [1, 2, 3, 4, 5, 6, 7] }, t)).toContain(
    el.share.everyday,
  );
});

// Monday, Wednesday, Friday is not a preset and must not be rounded up to one:
// somebody turning up on a Tuesday because the message said "καθημερινές" is
// the whole cost of guessing here.
test('days that are not a preset are listed as themselves, in order', () => {
  const message = shareRouteMessage({ ...weekdayRoute, weekdays: [5, 1, 3] }, t);

  expect(message).toContain('Δε Τε Πα');
  expect(message).not.toContain('καθημερινές');
});

// The caller's array belongs to react-query's cache. Sorting it in place would
// reorder the list a screen is drawing from.
test('the days the caller passed are left as they were', () => {
  const weekdays = [5, 1, 3];
  shareRouteMessage({ ...weekdayRoute, weekdays }, t);
  expect(weekdays).toEqual([5, 1, 3]);
});

test('a one-off route carries its date and no weekdays at all', () => {
  const message = shareRouteMessage(
    { ...weekdayRoute, kind: 'oneoff', weekdays: [], oneoff_date: '2026-09-21' },
    t,
  );

  expect(message).toContain('2026-09-21 07:30');
  expect(message).not.toMatch(/Δε|καθημερινές/);
});

// A one-off keeps its date even if weekdays were left behind by an earlier
// edit: the kind decides, not whichever field happens to be filled.
test('a one-off route ignores leftover weekdays', () => {
  const message = shareRouteMessage(
    { ...weekdayRoute, kind: 'oneoff', oneoff_date: '2026-09-21' },
    t,
  );

  expect(message).toContain('2026-09-21');
  expect(message).not.toContain('καθημερινές');
});

test('a recurring route ignores a leftover one-off date', () => {
  const message = shareRouteMessage({ ...weekdayRoute, oneoff_date: '2026-09-21' }, t);

  expect(message).not.toContain('2026-09-21');
  expect(message).toContain('καθημερινές');
});

// Malformed data says less rather than saying something false: no schedule, no
// empty separator where one would have been, and never the word "undefined".
test('a route with no schedule at all still reads as a sentence', () => {
  for (const broken of [
    { ...weekdayRoute, weekdays: [] },
    { ...weekdayRoute, kind: 'oneoff', weekdays: [], oneoff_date: undefined },
  ] as ShareableRoute[]) {
    const message = shareRouteMessage(broken, t);

    expect(message).toContain('Αλεξανδρούπολη → Σουφλί');
    expect(message).toContain('07:30');
    expect(message).not.toContain('undefined');
    expect(message).not.toMatch(/· *·/);
  }
});

test('one seat is one seat', () => {
  expect(shareRouteMessage({ ...weekdayRoute, seats: 1 }, t)).toContain(
    (el.share.seats_one as string).replace('{{count}}', '1'),
  );
});

// A full car is worth sharing anyway — the route is what somebody wants to know
// about for next week — but it must not promise room that is not there.
test('a route with no free seat does not promise one', () => {
  const message = shareRouteMessage({ ...weekdayRoute, seats: 0 }, t);

  expect(message).toContain('Αλεξανδρούπολη → Σουφλί');
  expect(message).not.toMatch(/θέσ/);
  expect(message).not.toMatch(/· *·/);
});

// A label that arrived empty would leave the message opening with an arrow.
test('an end with no name leaves no dangling arrow', () => {
  const message = shareRouteMessage({ ...weekdayRoute, destination_label: '  ' }, t);

  expect(message).not.toContain('→');
  expect(message).toContain('Αλεξανδρούπολη');
});

// Greek is what the pilot reads; English is the fallback, and a missing key
// there comes out as the key itself in the middle of a shared message.
test('every string the message needs exists in both languages', () => {
  const keys = ['weekdays', 'weekend', 'everyday', 'seats_one', 'seats_other', 'callToAction', 'button'];

  for (const k of keys) {
    expect(el.share).toHaveProperty(k);
    expect(en.share).toHaveProperty(k);
  }
});

test('English says it in English', () => {
  const message = shareRouteMessage(weekdayRoute, i18n.getFixedT('en'));

  expect(message).toContain(en.share.weekdays);
  expect(message).toMatch(/2 seats/);
});

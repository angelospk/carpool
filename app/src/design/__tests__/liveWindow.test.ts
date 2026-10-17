import { liveWindow, liveWindowOpen } from '../liveWindow';

const MIN = 60_000;
const trip = {
  depart_earliest: '2030-01-15T07:00:00Z',
  depart_latest: '2030-01-15T07:30:00Z',
  route_min: 20,
};
const at = (hhmm: string) => Date.parse(`2030-01-15T${hhmm}:00Z`);

test('opens 30 minutes before the earliest departure, closes an hour after the latest could arrive', () => {
  expect(liveWindow(trip)).toEqual({ opens: at('06:30'), closes: at('08:50') });
});

test('the start is in and the end is out', () => {
  expect(liveWindowOpen(trip, at('06:30') - 1)).toBe(false);
  expect(liveWindowOpen(trip, at('06:30'))).toBe(true);
  expect(liveWindowOpen(trip, at('08:50') - 1)).toBe(true);
  expect(liveWindowOpen(trip, at('08:50'))).toBe(false);
});

test('a corrupt route length is capped at six hours, as on the server', () => {
  const { closes } = liveWindow({ ...trip, route_min: 100_000 });
  expect(closes).toBe(at('07:30') + 360 * MIN + 60 * MIN);
});

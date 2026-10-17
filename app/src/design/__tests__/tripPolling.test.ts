import { tripPollInterval } from '../tripPolling';
import type { TripDetail } from '../../api/endpoints';

const NOW = Date.parse('2026-10-17T09:00:00Z'); // 12:00 in Athens

function trip(over: Partial<TripDetail>): TripDetail {
  return {
    id: 'o1',
    role: 'driver',
    status: 'scheduled',
    depart_earliest: '2026-10-18T05:00:00Z',
    depart_latest: '2026-10-18T05:30:00Z',
    passengers: [{ id: 'p1', can_rate: false, rated: false }],
    driver: { id: 'd1', can_rate: false, rated: false },
    confirmed_by_me: false,
    ...over,
  } as unknown as TripDetail;
}

describe('tripPollInterval', () => {
  // QA 2026-10-17 U F2: the driver's screen showed a passenger who had left
  // for as long as it stayed open.
  it('polls a trip of today or tomorrow that has somebody else on it', () => {
    expect(tripPollInterval(trip({}), NOW)).toBe(15_000);
    expect(tripPollInterval(trip({ role: 'passenger' }), NOW)).toBe(15_000);
    expect(
      tripPollInterval(
        trip({ depart_earliest: '2026-10-17T15:00:00Z', depart_latest: '2026-10-17T15:30:00Z' }),
        NOW,
      ),
    ).toBe(15_000);
  });

  // Codex review: a driver's empty trip gets its first passenger on another
  // device, and the change of a clock must not move "tomorrow".
  it('polls a driver trip with nobody on it yet, and tomorrow across the clock change', () => {
    expect(tripPollInterval(trip({ passengers: [] }), NOW)).toBe(15_000);
    // 00:30 on 25 October in Athens, the night the clocks go back.
    const lateNight = Date.parse('2026-10-24T21:30:00Z');
    expect(
      tripPollInterval(
        trip({ depart_earliest: '2026-10-26T05:00:00Z', depart_latest: '2026-10-26T05:30:00Z' }),
        lateNight,
      ),
    ).toBe(15_000);
  });

  it('leaves alone a trip further off and a cancelled one', () => {
    expect(
      tripPollInterval(
        trip({ depart_earliest: '2026-10-20T05:00:00Z', depart_latest: '2026-10-20T05:30:00Z' }),
        NOW,
      ),
    ).toBe(false);
    expect(tripPollInterval(trip({ status: 'cancelled' }), NOW)).toBe(false);
    expect(tripPollInterval(undefined, NOW)).toBe(false);
  });

  it('still polls slowly after the trip while somebody else has to confirm', () => {
    const done = trip({
      depart_earliest: '2026-10-15T05:00:00Z',
      depart_latest: '2026-10-15T05:30:00Z',
      confirmed_by_me: true,
    });
    expect(tripPollInterval(done, NOW)).toBe(30_000);
  });
});

import i18n from '../i18n';
import type { TripDetail } from '../api/endpoints';
import { tripShareMessage } from './tripShare';

const t = i18n.t.bind(i18n);

const trip = {
  id: 'occ-1',
  role: 'passenger',
  depart_earliest: '2026-07-30T04:30:00Z',
  depart_latest: '2026-07-30T05:00:00Z',
  origin_label: 'Αλεξανδρούπολη ',
  destination_label: 'Κομοτηνή',
  driver: { id: 'd', name: 'Γιώργος', car_model: 'Toyota Yaris', car_color: 'λευκό', car_plate: 'ΚΒΗ-1234' },
  passengers: [{ id: 'p', name: 'Μαρία' }],
} as unknown as TripDetail;

describe('the trip message for somebody you trust', () => {
  it('says who, in what car, from where to where, and when', () => {
    const msg = tripShareMessage(trip, t);
    expect(msg).toContain('Αλεξανδρούπολη → Κομοτηνή');
    expect(msg).toContain('07:30');
    expect(msg).toContain('Γιώργος');
    expect(msg).toContain('Toyota Yaris λευκό · ΚΒΗ-1234');
  });

  // A driver sharing their own trip is not asking a friend to find them by the
  // plate, and the passengers' names are theirs to give out.
  it('leaves the plate and the passengers out when the driver sends it', () => {
    const msg = tripShareMessage({ ...trip, role: 'driver' }, t);
    expect(msg).not.toContain('ΚΒΗ-1234');
    expect(msg).not.toContain('Μαρία');
    expect(msg).toContain('1 επιβάτη');
  });

  it('says less rather than leave a gap when the car is not known', () => {
    const msg = tripShareMessage(
      { ...trip, driver: { ...trip.driver, car_model: null, car_color: null, car_plate: null } },
      t,
    );
    expect(msg.split('\n')).toHaveLength(2);
  });
});

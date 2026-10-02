import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { Linking } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { OnTheRoadCard, onTheRoad } from '../OnTheRoadCard';
import { api, type TripDetail } from '../../api/endpoints';
import { stopDriveTracking } from '../../drive/tracking';

jest.mock('../../api/endpoints', () => ({
  api: { adjustDrive: jest.fn(() => Promise.resolve()), endDrive: jest.fn(() => Promise.resolve()) },
}));
jest.mock('../../drive/tracking', () => ({ stopDriveTracking: jest.fn(() => Promise.resolve()) }));
jest.mock('../../drive/mapsApp', () => ({ getMapsApp: jest.fn(() => Promise.resolve('google')) }));

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const at = (hhmmUTC: string) => Date.parse(`2026-08-04T${hhmmUTC}:00Z`);

function trip(over: Partial<TripDetail> = {}): TripDetail {
  return {
    id: 'trip-1',
    role: 'driver',
    status: 'scheduled',
    depart_earliest: '2026-08-04T04:00:00Z',
    depart_latest: '2026-08-04T04:00:00Z',
    drive_set_at: '2026-08-04T03:55:00Z',
    drive_ended_at: null,
    offers_until: '2026-08-04T04:20:00Z',
    detour_min: 5,
    destination: [26.0, 40.86],
    passengers: [
      { id: 'u1', name: 'Μαρία', pickup: [25.9, 40.85], arrived_at: '2026-08-04T04:04:00Z' },
      { id: 'u2', name: 'Νίκος', pickup: [25.95, 40.85], arrived_at: null },
    ],
    ...over,
  } as TripDetail;
}

beforeEach(() => jest.clearAllMocks());

test("is the driver's once the pinned minute has passed, until «Τέλος»", () => {
  expect(onTheRoad(trip(), at('03:59'))).toBe(false);
  expect(onTheRoad(trip(), at('04:00'))).toBe(true);
  expect(onTheRoad(trip({ drive_ended_at: '2026-08-04T04:30:00Z' }), at('04:31'))).toBe(false);
  expect(onTheRoad(trip({ role: 'passenger' }), at('04:05'))).toBe(false);
  expect(onTheRoad(trip({ drive_set_at: null }), at('04:05'))).toBe(false);
});

test('says until when it takes people, and stops with one tap', async () => {
  const onChanged = jest.fn();
  render(wrap(<OnTheRoadCard trip={trip()} now={() => at('04:05')} onChanged={onChanged} />));
  expect(screen.getByTestId('road-card')).toHaveTextContent(/07:20/);
  fireEvent(screen.getByTestId('road-offering'), 'valueChange', false);
  await waitFor(() => expect(api.adjustDrive).toHaveBeenCalledWith('trip-1', { offering: false }));
  await waitFor(() => expect(onChanged).toHaveBeenCalled());
});

test('shows who is already at their pickup', () => {
  render(wrap(<OnTheRoadCard trip={trip()} now={() => at('04:05')} onChanged={jest.fn()} />));
  expect(screen.getByTestId('road-passenger-u1')).toHaveTextContent(/στο σημείο/);
  expect(screen.getByTestId('road-passenger-u2')).not.toHaveTextContent(/στο σημείο/);
});

test("hands one passenger's pickup to the maps app", async () => {
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(true);
  render(wrap(<OnTheRoadCard trip={trip()} now={() => at('04:05')} onChanged={jest.fn()} />));
  fireEvent.press(screen.getByTestId('road-navigate-u2'));
  await waitFor(() => expect(open).toHaveBeenCalled());
  expect(open.mock.calls[0][0]).toContain('waypoints=40.85%2C25.95');
});

test('«Τέλος» ends the drive and stops sharing the car', async () => {
  const onChanged = jest.fn();
  render(wrap(<OnTheRoadCard trip={trip()} now={() => at('04:05')} onChanged={onChanged} />));
  fireEvent.press(screen.getByTestId('road-end'));
  await waitFor(() => expect(api.endDrive).toHaveBeenCalledWith('trip-1'));
  await waitFor(() => expect(stopDriveTracking).toHaveBeenCalledWith('trip-1'));
  expect(onChanged).toHaveBeenCalled();
});

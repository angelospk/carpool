import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { DriveCard, driveCardShown } from '../DriveCard';
import { api, type TripDetail } from '../../api/endpoints';
import { ApiError } from '../../api/client';

jest.mock('../../api/endpoints', () => ({ api: { startDrive: jest.fn() } }));
const mockTrack = jest.fn((_trip: string, _notice: unknown) => Promise.resolve('started'));
jest.mock('../../drive/tracking', () => ({
  startDriveTracking: (trip: string, notice: unknown) => mockTrack(trip, notice),
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

// 07:00–07:30 Athens on 4 August 2026 is 04:00–04:30 UTC.
const opens = '2026-08-04T04:00:00Z';
const closes = '2026-08-04T04:30:00Z';
const at = (hhmmUTC: string) => Date.parse(`2026-08-04T${hhmmUTC}:00Z`);

function trip(over: Partial<TripDetail> = {}): TripDetail {
  return {
    id: 'trip-1',
    role: 'driver',
    status: 'scheduled',
    depart_earliest: opens,
    depart_latest: closes,
    detour_min: 5,
    drive_set_at: null,
    ...over,
  } as TripDetail;
}

beforeEach(() => jest.clearAllMocks());

test('is offered to the driver from an hour before the window to an hour after', () => {
  expect(driveCardShown(trip(), at('02:59'))).toBe(false);
  expect(driveCardShown(trip(), at('03:00'))).toBe(true);
  expect(driveCardShown(trip(), at('05:29'))).toBe(true);
  expect(driveCardShown(trip(), at('05:31'))).toBe(false);
  expect(driveCardShown(trip({ role: 'passenger' }), at('03:50'))).toBe(false);
  expect(driveCardShown(trip({ status: 'cancelled' }), at('03:50'))).toBe(false);
});

// Pinned and gone is gone: changing the minute after it is the moving drive,
// which this card is not.
test('goes once the pinned minute has passed', () => {
  const pinned = trip({
    drive_set_at: '2026-08-04T03:55:00Z',
    depart_earliest: '2026-08-04T04:05:00Z',
    depart_latest: '2026-08-04T04:05:00Z',
    scheduled_earliest: opens,
    scheduled_latest: closes,
  });
  expect(driveCardShown(pinned, at('04:04'))).toBe(true);
  expect(driveCardShown(pinned, at('04:05'))).toBe(false);
});

test('sends the minute picked and the detour, as a time', async () => {
  (api.startDrive as jest.Mock).mockResolvedValue({
    departs_at: '2026-08-04T04:00:00Z',
  });
  const onDone = jest.fn();
  render(wrap(<DriveCard trip={trip()} now={() => at('03:50')} onDone={onDone} />));

  fireEvent.press(screen.getByTestId('drive-when-10'));
  fireEvent.press(screen.getByTestId('drive-detour-0'));
  fireEvent.press(screen.getByTestId('drive-go'));

  await waitFor(() => expect(api.startDrive).toHaveBeenCalledWith('trip-1', '2026-08-04T04:00:00.000Z', 0));
  await waitFor(() => expect(onDone).toHaveBeenCalled());
});

test('starts from the detour the trip already has', () => {
  render(wrap(<DriveCard trip={trip({ detour_min: 3 })} now={() => at('03:50')} onDone={jest.fn()} />));
  expect(screen.getByTestId('drive-detour-3').props.accessibilityState?.selected).toBe(true);
  expect(screen.getByTestId('drive-when-0').props.accessibilityState?.selected).toBe(true);
});

test('says when the driver is leaving once they have said it', () => {
  const pinned = trip({
    drive_set_at: '2026-08-04T03:55:00Z',
    depart_earliest: '2026-08-04T04:05:00Z',
    depart_latest: '2026-08-04T04:05:00Z',
  });
  render(wrap(<DriveCard trip={pinned} now={() => at('03:58')} onDone={jest.fn()} />));
  expect(screen.getByTestId('drive-card')).toHaveTextContent(/07:05/);
});

test('a refusal is said in words', async () => {
  (api.startDrive as jest.Mock).mockRejectedValue(new ApiError(422, 'outside_window', 'x'));
  render(wrap(<DriveCard trip={trip()} now={() => at('03:50')} onDone={jest.fn()} />));
  fireEvent.press(screen.getByTestId('drive-go'));
  expect(await screen.findByTestId('drive-error')).toHaveTextContent(/δημοσιευμένη ώρα/);
});

// «Φεύγω» is also when the car starts being shared, so the people ahead of it
// can be timed once it has left.
test('leaving starts sharing the car', async () => {
  (api.startDrive as jest.Mock).mockResolvedValue({ departs_at: '2026-08-04T03:50:00Z' });
  render(wrap(<DriveCard trip={trip()} now={() => at('03:50')} onDone={jest.fn()} />));
  fireEvent.press(screen.getByTestId('drive-go'));
  await waitFor(() => expect(mockTrack).toHaveBeenCalledWith('trip-1', expect.anything()));
});

test('a refused location permission is explained, and the drive still stands', async () => {
  (api.startDrive as jest.Mock).mockResolvedValue({ departs_at: '2026-08-04T03:50:00Z' });
  mockTrack.mockResolvedValueOnce('denied');
  const onDone = jest.fn();
  render(wrap(<DriveCard trip={trip()} now={() => at('03:50')} onDone={onDone} />));
  fireEvent.press(screen.getByTestId('drive-go'));
  expect(await screen.findByTestId('drive-error')).toHaveTextContent(/θέση σου/);
  expect(onDone).toHaveBeenCalled();
});

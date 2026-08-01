import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { PastTrips } from '../../src/components/PastTrips';
import { ApiError } from '../../src/api/client';
import { api, type PastTrip } from '../../src/api/endpoints';

jest.mock('../../src/api/endpoints', () => ({
  api: { pastTrips: jest.fn(), again: jest.fn() },
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const ride = {
  id: 'occ-1',
  booking_id: 'bk-1',
  role: 'passenger',
  depart_earliest: '2026-07-21T04:30:00Z',
  depart_latest: '2026-07-21T05:00:00Z',
  status: 'completed',
  route_km: 28.6,
  origin_label: 'Αλεξ/πολη',
  destination_label: 'Φέρες',
  price_eur: 3.2,
  seats_free: 2,
  driver_name: 'Γιάννης',
} as unknown as PastTrip;

beforeEach(() => {
  jest.clearAllMocks();
  (api.pastTrips as jest.Mock).mockResolvedValue({ trips: [ride] });
});

test('one tap repeats the ride with the driver it was taken with', async () => {
  (api.again as jest.Mock).mockResolvedValue({ request_id: 'r1', ask_id: 'a1', asking: true });

  render(wrap(<PastTrips />));
  await waitFor(() => expect(screen.getByTestId('again-bk-1')).toBeTruthy());
  fireEvent.press(screen.getByTestId('again-bk-1'));

  await waitFor(() => expect(api.again).toHaveBeenCalledWith('bk-1'));
  await waitFor(() =>
    expect(String(screen.getByTestId('past-notice').props.children)).toContain('οδηγό'),
  );
});

// The tap does not book anything, so saying "done" would be a lie: the driver
// still has to answer. When there is no trip to ask about, that has to be said
// too, rather than leaving a button that appears to do nothing.
test('a route with no next trip says so instead of failing silently', async () => {
  (api.again as jest.Mock).mockRejectedValue(
    new ApiError(409, 'nothing_to_repeat', 'no next trip'),
  );

  render(wrap(<PastTrips />));
  await waitFor(() => expect(screen.getByTestId('again-bk-1')).toBeTruthy());
  fireEvent.press(screen.getByTestId('again-bk-1'));

  await waitFor(() =>
    expect(String(screen.getByTestId('past-notice').props.children)).toContain('ελεύθερη θέση'),
  );
});

test('a passenger with no history is shown nothing', async () => {
  (api.pastTrips as jest.Mock).mockResolvedValue({ trips: [] });

  const view = render(wrap(<PastTrips />));

  await waitFor(() => expect(api.pastTrips).toHaveBeenCalled());
  expect(view.toJSON()).toBeNull();
});

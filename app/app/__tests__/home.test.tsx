import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import HomeScreen from '../(tabs)/index';
import { api } from '../../src/api/endpoints';

const mockPush = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush, replace: jest.fn() }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { upcomingTrips: jest.fn(), trip: jest.fn() },
}));

jest.mock('../../src/state/session', () => ({
  useSession: (select: (s: unknown) => unknown) =>
    select({ me: { id: 'u1', name: 'Χαράλαμπος', photo_url: null } }),
}));

const trip = (over: Partial<Record<string, unknown>> = {}) => ({
  id: 't1',
  role: 'passenger' as const,
  depart_earliest: '2026-07-29T04:30:00Z', // 07:30 in Athens
  depart_latest: '2026-07-29T05:00:00Z',
  status: 'confirmed',
  route_km: 57.8,
  origin_label: 'Αλεξανδρούπολη',
  destination_label: 'Κομοτηνή',
  price_eur: 1,
  seats_free: 2,
  ...over,
});

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

beforeEach(() => {
  jest.clearAllMocks();
  (api.trip as jest.Mock).mockResolvedValue({
    ...trip(),
    // A short encoded polyline: two points near Alexandroupoli.
    route_polyline: '_p~iF~ps|U_ulLnnqC',
    driver: { id: 'd1', name: 'Οδηγός', photo_url: null, rating: null },
    passengers: [],
  });
});

// The morning question is "when do I leave", so the departure has to be on the
// card in Athens time — not the device's, and not after a tap.
test('shows the next departure in Athens time, with its route glyph', async () => {
  (api.upcomingTrips as jest.Mock).mockResolvedValue({ trips: [trip()] });

  render(wrap(<HomeScreen />));

  expect(await screen.findByText('07:30')).toBeTruthy();
  expect(screen.getByTestId('route-glyph')).toBeTruthy();
  expect(screen.getByTestId('seat-pips')).toBeTruthy();
});

test('opens a trip when its card is pressed', async () => {
  (api.upcomingTrips as jest.Mock).mockResolvedValue({ trips: [trip()] });

  render(wrap(<HomeScreen />));
  fireEvent.press(await screen.findByTestId('trip-t1'));

  await waitFor(() => expect(mockPush).toHaveBeenCalledWith('/trip/t1'));
});

test('says so when there is nothing coming up', async () => {
  (api.upcomingTrips as jest.Mock).mockResolvedValue({ trips: [] });

  render(wrap(<HomeScreen />));

  expect(await screen.findByTestId('empty')).toBeTruthy();
});

// Only the next trip gets the hero treatment; the rest are rows. Rendering two
// heroes would push the second trip off the screen.
test('gives the hero card to the next trip only', async () => {
  (api.upcomingTrips as jest.Mock).mockResolvedValue({
    trips: [trip(), trip({ id: 't2', depart_earliest: '2026-07-30T05:30:00Z' })],
  });

  render(wrap(<HomeScreen />));
  await screen.findByTestId('trip-t1');

  expect(screen.getAllByTestId('route-glyph')).toHaveLength(1);
  expect(screen.getByTestId('trip-t2')).toBeTruthy();
});

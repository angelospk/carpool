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
  api: { upcomingTrips: jest.fn(), pastTrips: jest.fn(), trip: jest.fn(), myRequests: jest.fn() },
}));

// Prefixed `mock` because jest.mock's factory may not close over anything else.
let mockSessionStatus: 'loading' | 'signedOut' | 'signedIn' = 'signedIn';

jest.mock('../../src/state/session', () => ({
  useSession: (select: (s: unknown) => unknown) =>
    select({
      me: { id: 'u1', name: 'Χαράλαμπος', photo_url: null },
      get status() {
        return mockSessionStatus;
      },
    }),
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
  // The screen lists what is still being waited on as well as what is booked,
  // and an empty screen is only empty when both of them are.
  (api.myRequests as jest.Mock).mockResolvedValue({ requests: [] });
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
  // In words, not in pips. The pips drew a total of `seats_free + 1`, so a
  // five-seater with two seats left rendered as a three-seater.
  expect(screen.getByTestId('seats-free')).toHaveTextContent(/2/);
});

// Somebody opening the app for the first time has nothing booked and no
// history, which used to produce "δεν έχεις επόμενες διαδρομές" and no way in.
test('an account with nothing on it is offered both sides of the deal', async () => {
  (api.upcomingTrips as jest.Mock).mockResolvedValue({ trips: [] });
  (api.pastTrips as jest.Mock).mockResolvedValue({ trips: [] });

  render(wrap(<HomeScreen />));

  fireEvent.press(await screen.findByTestId('empty-ask'));
  await waitFor(() => expect(mockPush).toHaveBeenCalledWith('/search'));

  fireEvent.press(screen.getByTestId('empty-offer'));
  await waitFor(() => expect(mockPush).toHaveBeenCalledWith('/publish'));
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


// The tab navigator mounts before the session is settled. Asking the server for
// this user's trips before there is a user gets two 401s on every cold start —
// harmless but wasteful, and it buries any real error in noise.
describe('before the session is settled', () => {
  afterEach(() => {
    mockSessionStatus = 'signedIn';
  });

  it.each(['loading', 'signedOut'] as const)('asks for nothing while %s', async (status) => {
    mockSessionStatus = status;
    (api.upcomingTrips as jest.Mock).mockResolvedValue({ trips: [] });
    (api.pastTrips as jest.Mock).mockResolvedValue({ trips: [] });

    render(wrap(<HomeScreen />));
    await waitFor(() => expect(screen.queryByText('Χαράλαμπος')).toBeTruthy());

    expect(api.upcomingTrips).not.toHaveBeenCalled();
    expect(api.pastTrips).not.toHaveBeenCalled();
    expect(api.myRequests).not.toHaveBeenCalled();
  });
});

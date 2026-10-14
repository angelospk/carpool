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
  api: {
    upcomingTrips: jest.fn(),
    pastTrips: jest.fn(),
    trip: jest.fn(),
    myRequests: jest.fn(),
    pendingAsks: jest.fn(async () => ({ asks: [] })),
  },
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

// The driving prompt has tests of its own; here it is in the way.
jest.mock('../../src/components/DrivingPrompt', () => ({ DrivingPrompt: () => null }));

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
  (api.upcomingTrips as jest.Mock).mockResolvedValue({ trips: [trip({ role: 'driver' })] });

  render(wrap(<HomeScreen />));

  expect(await screen.findByText('07:30')).toBeTruthy();
  expect(screen.getByTestId('route-glyph')).toBeTruthy();
  // A driver's seats, in words, not in pips. The pips drew a total of `seats_free + 1`, so a
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


// A passenger's next trip is somebody else's car. Who is driving is the thing
// they check; how many seats the driver still has free is the driver's business.
test('a passenger next trip names the driver instead of the free seats', async () => {
  (api.upcomingTrips as jest.Mock).mockResolvedValue({ trips: [trip()] });
  (api.trip as jest.Mock).mockResolvedValue({
    ...trip(),
    route_polyline: '_p~iF~ps|U_ulLnnqC',
    driver: { id: 'd1', name: 'Μαρία', photo_url: null, rating: null },
    passengers: [],
  });

  render(wrap(<HomeScreen />));

  expect(await screen.findByText('Οδηγός: Μαρία')).toBeTruthy();
  expect(screen.queryByTestId('seats-free')).toBeNull();
});

test('a driver next trip still says how many seats are free', async () => {
  (api.upcomingTrips as jest.Mock).mockResolvedValue({ trips: [trip({ role: 'driver' })] });

  render(wrap(<HomeScreen />));

  expect(await screen.findByTestId('seats-free')).toBeTruthy();
});

// «2.37 €» beside «1,00 €» on the next screen: one shape, the Greek one.
test('writes the contribution and the distance with a decimal comma', async () => {
  (api.upcomingTrips as jest.Mock).mockResolvedValue({ trips: [trip({ price_eur: 2.37 })] });

  render(wrap(<HomeScreen />));

  expect(await screen.findByText('2,37\u00a0€')).toBeTruthy();
  expect(screen.getByText('57,8\u00a0χλμ')).toBeTruthy();
});

// The first-run card offers both sides of the deal in full; the two pills in
// the header said the same thing a second time, and the tab bar a third.
test('an empty account is offered each action once, not twice', async () => {
  (api.upcomingTrips as jest.Mock).mockResolvedValue({ trips: [] });
  (api.pastTrips as jest.Mock).mockResolvedValue({ trips: [] });

  render(wrap(<HomeScreen />));

  expect(await screen.findByTestId('empty-ask')).toBeTruthy();
  expect(screen.queryByTestId('quick-ask')).toBeNull();
  expect(screen.queryByTestId('quick-offer')).toBeNull();
});

test('somebody with trips keeps the quick actions', async () => {
  (api.upcomingTrips as jest.Mock).mockResolvedValue({ trips: [trip()] });

  render(wrap(<HomeScreen />));

  expect(await screen.findByTestId('quick-ask')).toBeTruthy();
});

test('departures past the coming week are counted, not listed', async () => {
  const inDays = (d: number) => new Date(Date.now() + d * 86_400_000).toISOString();
  (api.upcomingTrips as jest.Mock).mockResolvedValue({
    trips: [
      trip({ id: 'n', role: 'driver', depart_earliest: inDays(1) }),
      trip({ id: 'soon', role: 'driver', depart_earliest: inDays(2) }),
      trip({ id: 'far1', role: 'driver', depart_earliest: inDays(10) }),
      trip({ id: 'far2', role: 'driver', depart_earliest: inDays(12) }),
    ],
  });

  render(wrap(<HomeScreen />));

  expect(await screen.findByTestId('trip-soon')).toBeTruthy();
  expect(screen.queryByTestId('trip-far1')).toBeNull();
  expect(String(screen.getByTestId('later-count').props.children)).toContain('2');
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

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
    supply: jest.fn(async () => ({ status: 'unknown' })),
    pendingAsks: jest.fn(async () => ({ asks: [] })),
    pastTrips: jest.fn(),
    trip: jest.fn(),
    myRequests: jest.fn(),
    again: jest.fn(),
  },
}));

jest.mock('../../src/state/session', () => ({
  useSession: (select: (s: unknown) => unknown) =>
    select({ me: { id: 'u1', name: 'Χαράλαμπος', photo_url: null }, status: 'signedIn' }),
}));

const request = (over: Partial<Record<string, unknown>> = {}) => ({
  id: 'q1',
  origin_label: 'Αλεξανδρούπολη',
  destination_label: 'Κομοτηνή',
  window_start: '2026-07-29T04:30:00Z',
  window_end: '2026-07-29T06:30:00Z',
  seats: 1,
  status: 'asking' as const,
  drivers_asked: 0,
  drivers_declined: 0,
  asking_now: false,
  created_at: '2026-07-29T04:00:00Z',
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
  (api.upcomingTrips as jest.Mock).mockResolvedValue({ trips: [] });
  (api.pastTrips as jest.Mock).mockResolvedValue({ trips: [] });
});

// Somebody whose only activity is a request they are waiting on must not be
// told they have nothing, with the request hidden underneath the sentence.
test('a request on its own is not an empty screen', async () => {
  (api.myRequests as jest.Mock).mockResolvedValue({ requests: [request()] });

  render(wrap(<HomeScreen />));

  expect(await screen.findByTestId('request-q1')).toBeTruthy();
  expect(screen.queryByTestId('empty')).toBeNull();
});

test('pressing a request opens it', async () => {
  (api.myRequests as jest.Mock).mockResolvedValue({ requests: [request()] });

  render(wrap(<HomeScreen />));
  fireEvent.press(await screen.findByTestId('request-q1'));

  await waitFor(() => expect(mockPush).toHaveBeenCalledWith('/request/q1'));
});

// Four kinds of nothing, and they are not the same news.
describe('what the waiting actually is', () => {
  const progress = async (over: Partial<Record<string, unknown>>) => {
    (api.myRequests as jest.Mock).mockResolvedValue({ requests: [request(over)] });
    render(wrap(<HomeScreen />));
    return (await screen.findByTestId('request-q1-progress')).props.children as string;
  };

  test('nobody asked yet says so', async () => {
    expect(await progress({})).toMatch(/κανένας οδηγός ακόμα/);
  });

  test('somebody reading it now says so', async () => {
    expect(await progress({ drivers_asked: 1, asking_now: true })).toMatch(/Ρωτάμε/);
  });

  test('a search moving on counts the people who could not', async () => {
    const line = await progress({ drivers_asked: 2, drivers_declined: 2, asking_now: false });
    expect(line).toMatch(/Ψάχνουμε/);
    expect(line).toMatch(/2 οδηγοί/);
  });

  test('one driver who could not is said in the singular', async () => {
    const line = await progress({ drivers_asked: 1, drivers_declined: 1, asking_now: false });
    expect(line).toMatch(/1 οδηγός είπε/);
  });

  test('an ask that went out and has not come back is waiting, not searching', async () => {
    expect(await progress({ drivers_asked: 1, asking_now: false })).toMatch(/Περιμένουμε/);
  });
});

// A list that did not load is not a list with nothing in it — and it is not a
// reason to hide the trips that did load.
test('requests failing does not take the trips down with it', async () => {
  (api.myRequests as jest.Mock).mockRejectedValue(new Error('offline'));
  (api.upcomingTrips as jest.Mock).mockResolvedValue({
    trips: [
      {
        id: 't1',
        role: 'passenger',
        depart_earliest: '2026-07-29T04:30:00Z',
        depart_latest: '2026-07-29T05:00:00Z',
        status: 'confirmed',
        route_km: 57.8,
        origin_label: 'Αλεξανδρούπολη',
        destination_label: 'Κομοτηνή',
        price_eur: 1,
        seats_free: 2,
      },
    ],
  });
  (api.trip as jest.Mock).mockResolvedValue({
    route_polyline: '_p~iF~ps|U_ulLnnqC',
    driver: { id: 'd1', name: 'Οδηγός', photo_url: null, rating: null },
    passengers: [],
  });

  render(wrap(<HomeScreen />));

  expect(await screen.findByTestId('trip-t1')).toBeTruthy();
  expect(await screen.findByTestId('requests-failed')).toBeTruthy();
});

// The journeys somebody keeps making, one tap from being asked for again.
describe('routes you take often', () => {
  const past = (over: Partial<Record<string, unknown>> = {}) => ({
    id: 'p1',
    booking_id: 'b1',
    role: 'passenger' as const,
    depart_earliest: '2026-07-01T04:30:00Z',
    depart_latest: '2026-07-01T05:00:00Z',
    status: 'completed',
    route_km: 57.8,
    origin_label: 'Αλεξανδρούπολη',
    destination_label: 'Κομοτηνή',
    price_eur: 2.5,
    seats_free: 2,
    driver_name: 'Οδηγός',
    origin_lon: 25.8743,
    origin_lat: 40.8476,
    destination_lon: 25.4054,
    destination_lat: 41.1223,
    ...over,
  });

  test('a past journey is offered again, with both pins in the link', async () => {
    (api.myRequests as jest.Mock).mockResolvedValue({ requests: [] });
    (api.pastTrips as jest.Mock).mockResolvedValue({ trips: [past()] });

    render(wrap(<HomeScreen />));
    // By test id rather than by the text on it: the same journey is also in the
    // list of past rides directly above, with the same two names on it.
    fireEvent.press(await screen.findByTestId(/^suggestion-/));

    await waitFor(() => expect(mockPush).toHaveBeenCalled());
    const { pathname, params } = mockPush.mock.calls.at(-1)?.[0] as {
      pathname: string;
      params: Record<string, string>;
    };
    expect(pathname).toBe('/(tabs)/search');
    expect(params.from_label).toBe('Αλεξανδρούπολη');
    expect(params.to_lat).toBe('41.1223');
    expect(params.preset).toBeTruthy();
  });
});

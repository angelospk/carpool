import { render, screen, fireEvent, waitFor, within } from '@testing-library/react-native';
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
    weekendPlans: jest.fn(),
    offers: jest.fn(),
  },
}));

jest.mock('../../src/state/session', () => ({
  useSession: (select: (s: unknown) => unknown) =>
    select({ me: { id: 'u1', name: 'Χαράλαμπος', photo_url: null }, status: 'signedIn' }),
}));
jest.mock('../../src/components/DrivingPrompt', () => ({ DrivingPrompt: () => null }));

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const plan = {
  id: 'plan-1',
  origin_lon: 25.87,
  origin_lat: 40.84,
  destination_lon: 22.94,
  destination_lat: 40.64,
  origin_label: 'Αλεξανδρούπολη',
  destination_label: 'Θεσσαλονίκη',
  depart_dow: 5,
  depart_time: '15:00',
  return_dow: null,
  return_time: null,
  seats: 1,
  luggage: 'none',
  luggage_count: 0,
  every_week: true,
  declines_in_row: 0,
  this_week: 'none',
  ask_to_stop: false,
};

beforeEach(() => {
  jest.clearAllMocks();
  (api.upcomingTrips as jest.Mock).mockResolvedValue({ trips: [] });
  (api.pastTrips as jest.Mock).mockResolvedValue({ trips: [] });
  (api.myRequests as jest.Mock).mockResolvedValue({ requests: [] });
  (api.weekendPlans as jest.Mock).mockResolvedValue({ plans: [] });
  (api.offers as jest.Mock).mockResolvedValue({ offers: [] });
});

test('someone with trips has the weekend way in beside their requests', async () => {
  (api.upcomingTrips as jest.Mock).mockResolvedValue({
    trips: [
      {
        id: 't1',
        role: 'passenger',
        depart_earliest: '2099-07-29T04:30:00Z',
        depart_latest: '2099-07-29T05:00:00Z',
        status: 'confirmed',
        route_km: 50,
        origin_label: 'Α',
        destination_label: 'Β',
        price_eur: 1,
        seats_free: 1,
      },
    ],
  });
  (api.trip as jest.Mock).mockResolvedValue({ route_polyline: '', driver: { name: 'Δ' }, passengers: [] });
  render(wrap(<HomeScreen />));

  fireEvent.press(await screen.findByTestId('weekend-new'));

  expect(mockPush).toHaveBeenCalledWith('/weekend/new');
});

test('the first-run screen offers it too', async () => {
  render(wrap(<HomeScreen />));

  fireEvent.press(await screen.findByTestId('empty-weekend'));

  expect(mockPush).toHaveBeenCalledWith('/weekend/new');
});

// Until the hourly job makes its requests a plan is all there is, and «δεν
// έχεις επόμενες διαδρομές» over it would say the plan was not saved.
test('a plan keeps the home screen from reading as empty', async () => {
  (api.weekendPlans as jest.Mock).mockResolvedValue({ plans: [plan] });
  render(wrap(<HomeScreen />));

  expect(await screen.findByTestId('weekend-plan-plan-1')).toBeTruthy();
  expect(screen.queryByTestId('empty')).toBeNull();
});

test('a driver’s offer is on the home screen', async () => {
  (api.offers as jest.Mock).mockResolvedValue({
    offers: [
      {
        id: 'ask-1',
        request_id: 'r1',
        occurrence_id: 'occ-1',
        driver_name: 'Γιώργος',
        driver_photo_url: null,
        driver_rating: null,
        driver_rating_count: 0,
        car: { make: 'Toyota', model: 'Yaris', colour: '' },
        depart_earliest: '2099-10-16T12:00:00Z',
        price_eur: 8,
        seats: 1,
        detour_min: 0,
        detour_km: 0,
        origin_label: 'Αλεξανδρούπολη',
        destination_label: 'Θεσσαλονίκη',
        expires_at: '2099-10-16T09:00:00Z',
      },
    ],
  });
  render(wrap(<HomeScreen />));

  expect(await screen.findByTestId('seat-offer-ask-1')).toBeTruthy();
  await waitFor(() => expect(api.offers).toHaveBeenCalled());
});

// Offers are amber and several can stack: outside the list they pushed the
// trips off the screen with nothing to scroll.
test('the offers scroll with the rest of the home screen', async () => {
  const offer = (id: string) => ({
    id,
    request_id: `r-${id}`,
    occurrence_id: 'occ-1',
    driver_name: 'Γιώργος',
    driver_photo_url: null,
    driver_rating: null,
    driver_rating_count: 0,
    car: { make: 'Toyota', model: 'Yaris', colour: '' },
    depart_earliest: '2099-10-16T12:00:00Z',
    price_eur: 8,
    seats: 1,
    detour_min: 0,
    detour_km: 0,
    origin_label: 'Αλεξανδρούπολη',
    destination_label: 'Θεσσαλονίκη',
    expires_at: '2099-10-16T09:00:00Z',
  });
  (api.offers as jest.Mock).mockResolvedValue({ offers: [offer('ask-1'), offer('ask-2')] });
  (api.upcomingTrips as jest.Mock).mockResolvedValue({
    trips: [
      {
        id: 't1',
        role: 'passenger',
        depart_earliest: '2099-07-29T04:30:00Z',
        depart_latest: '2099-07-29T05:00:00Z',
        status: 'confirmed',
        route_km: 50,
        origin_label: 'Α',
        destination_label: 'Β',
        price_eur: 1,
        seats_free: 1,
      },
    ],
  });
  render(wrap(<HomeScreen />));

  const list = await screen.findByTestId('trip-list');
  expect(await within(list).findByTestId('seat-offers')).toBeTruthy();
  expect(within(list).getByTestId('seat-offer-ask-2')).toBeTruthy();
});

// An offer is something to answer, so it must not vanish into the first-run
// screen when it is all the passenger has.
test('an offer alone is still inside the list, not under «δεν έχεις»', async () => {
  (api.offers as jest.Mock).mockResolvedValue({
    offers: [
      {
        id: 'ask-1',
        request_id: 'r1',
        occurrence_id: 'occ-1',
        driver_name: 'Γιώργος',
        driver_photo_url: null,
        driver_rating: null,
        driver_rating_count: 0,
        car: { make: 'Toyota', model: 'Yaris', colour: '' },
        depart_earliest: '2099-10-16T12:00:00Z',
        price_eur: 8,
        seats: 1,
        detour_min: 0,
        detour_km: 0,
        origin_label: 'Α',
        destination_label: 'Β',
        expires_at: '2099-10-16T09:00:00Z',
      },
    ],
  });
  render(wrap(<HomeScreen />));

  const list = await screen.findByTestId('trip-list');
  expect(await within(list).findByTestId('seat-offer-ask-1')).toBeTruthy();
  expect(screen.queryByTestId('empty-weekend')).toBeNull();
});

// The trips failing to load says nothing about the offers, which lapse: they
// stay on the screen, with the trips' error beside them.
test('an offer stays on the screen when the trips fail to load', async () => {
  (api.upcomingTrips as jest.Mock).mockRejectedValue(new Error('offline'));
  (api.offers as jest.Mock).mockResolvedValue({
    offers: [
      {
        id: 'ask-1',
        request_id: 'r1',
        occurrence_id: 'occ-1',
        driver_name: 'Γιώργος',
        driver_photo_url: null,
        driver_rating: null,
        driver_rating_count: 0,
        car: { make: 'Toyota', model: 'Yaris', colour: '' },
        depart_earliest: '2099-10-16T12:00:00Z',
        price_eur: 8,
        seats: 1,
        detour_min: 0,
        detour_km: 0,
        origin_label: 'Α',
        destination_label: 'Β',
        expires_at: '2099-10-16T09:00:00Z',
      },
    ],
  });
  render(wrap(<HomeScreen />));

  const fallback = await screen.findByTestId('offers-fallback');
  expect(await within(fallback).findByTestId('seat-offer-ask-1')).toBeTruthy();
});

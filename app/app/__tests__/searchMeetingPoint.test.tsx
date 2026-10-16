import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import SearchScreen from '../(tabs)/search';
import { api } from '../../src/api/endpoints';

// The pickup was always the middle of the village. Φέρες sits 4 km off the
// Ορεστιάδα→Αλεξανδρούπολη road and never matched a driver who passes it
// (QA 2026-10-16, U4). A passenger can now say where exactly, as a driver can.
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
  useLocalSearchParams: () => ({}),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: {
    places: jest.fn(),
    placeNear: jest.fn(),
    meetingPoints: jest.fn(),
    proposeMeetingPoint: jest.fn(),
    createRequest: jest.fn(),
    estimate: jest.fn(),
    liveMap: jest.fn(),
    supply: jest.fn(),
  },
  LUGGAGE_SIZES: ['none', 'cabin', 'suitcase', 'bulky'],
}));

jest.mock('../../src/push/reach', () => ({
  usePushReach: (select: (s: unknown) => unknown) => select({ on: true }),
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const pin = (label: string, lon: number, lat: number) => ({
  id: `node/${label}`,
  area: 'Δοκιμής',
  kind: 'town' as const,
  name: label,
  lon,
  lat,
});

const fuel = {
  id: 'node/77',
  place_id: 'node/Φέρες',
  kind: 'fuel',
  name: 'Βενζινάδικο στην εθνική',
  lon: 26.15,
  lat: 40.91,
};

beforeEach(() => {
  jest.clearAllMocks();
  (api.liveMap as jest.Mock).mockResolvedValue({ trips: [] });
  (api.estimate as jest.Mock).mockRejectedValue(new Error('offline'));
  (api.supply as jest.Mock).mockResolvedValue({ status: 'unknown' });
  (api.createRequest as jest.Mock).mockResolvedValue({ request_id: 'q1', asking: false });
  (api.places as jest.Mock).mockImplementation(async (q: string) => ({
    places: q.startsWith('φε') ? [pin('Φέρες', 26.17, 40.9)] : [pin('Αλεξανδρούπολη', 25.87, 40.84)],
  }));
  (api.meetingPoints as jest.Mock).mockImplementation(async (place: string) =>
    place === 'node/Φέρες' ? [fuel] : [],
  );
});

async function choose(field: string, typed: string, name: string) {
  fireEvent.changeText(screen.getByTestId(`${field}-input`), typed);
  await waitFor(() => screen.getByTestId(`${field}-node/${name}`));
  fireEvent.press(screen.getByTestId(`${field}-node/${name}`));
}

test('a meeting point is where the passenger is picked up', async () => {
  render(wrap(<SearchScreen />));
  await choose('from', 'φερ', 'Φέρες');
  await choose('to', 'αλεξ', 'Αλεξανδρούπολη');

  fireEvent.press(await screen.findByTestId('from-point-node/77'));
  fireEvent.press(screen.getByTestId('search-submit'));

  await waitFor(() => expect(api.createRequest).toHaveBeenCalled());
  expect((api.createRequest as jest.Mock).mock.calls[0][0]).toEqual(
    expect.objectContaining({
      origin_lon: 26.15,
      origin_lat: 40.91,
      origin_label: 'Φέρες, Βενζινάδικο στην εθνική',
      destination_lon: 25.87,
    }),
  );
});

test('without one, the pickup is the place itself', async () => {
  render(wrap(<SearchScreen />));
  await choose('from', 'φερ', 'Φέρες');
  await choose('to', 'αλεξ', 'Αλεξανδρούπολη');

  fireEvent.press(screen.getByTestId('search-submit'));

  await waitFor(() => expect(api.createRequest).toHaveBeenCalled());
  expect((api.createRequest as jest.Mock).mock.calls[0][0]).toEqual(
    expect.objectContaining({ origin_lon: 26.17, origin_lat: 40.9, origin_label: 'Φέρες' }),
  );
});

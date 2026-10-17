import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import PublishScreen from '../(tabs)/publish';
import { api } from '../../src/api/endpoints';

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
  useFocusEffect: (effect: () => void) => require('react').useEffect(effect, []),
  useLocalSearchParams: () => ({}),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: {
    publishRoute: jest.fn(),
    places: jest.fn(),
    meetingPoints: jest.fn(),
    vehicles: jest.fn(),
    myGroups: jest.fn(),
    shareRouteLink: jest.fn(),
    upcomingTrips: jest.fn(),
    waiting: jest.fn(),
    offerSeat: jest.fn(),
  },
  LUGGAGE_SIZES: ['none', 'cabin', 'suitcase', 'bulky'],
}));

jest.mock('../../src/state/session', () => ({
  useSession: (select: (s: unknown) => unknown) => select({ status: 'signedIn' }),
}));

const pin = (label: string, lon: number, lat: number) => ({
  id: `node/${label}`,
  area: 'Δοκιμής',
  kind: 'town' as const,
  name: label,
  lon,
  lat,
});

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false }, queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

beforeEach(() => {
  jest.clearAllMocks();
  (api.vehicles as jest.Mock).mockResolvedValue([
    { id: 'v1', name: 'Toyota Yaris', label: '', make: 'Toyota', model: 'Yaris', fuel: 'petrol', colour: 'λευκό', plate: 'ΑΒΓ-1234', is_default: true },
  ]);
  (api.myGroups as jest.Mock).mockResolvedValue({ groups: [] });
  (api.shareRouteLink as jest.Mock).mockRejectedValue(new Error('offline'));
  (api.places as jest.Mock).mockResolvedValue({
    places: [pin('Αλεξανδρούπολη', 25.87, 40.84), pin('Θεσσαλονίκη', 22.94, 40.64)],
  });
  (api.meetingPoints as jest.Mock).mockResolvedValue({ points: [] });
  (api.publishRoute as jest.Mock).mockResolvedValue({ template_id: 'r1', route_km: 300, ends_at_port: false });
  // 2099-01-02 is a Friday.
  (api.upcomingTrips as jest.Mock).mockResolvedValue({
    trips: [
      {
        id: 'occ-fri',
        role: 'driver',
        depart_earliest: '2099-01-02T12:00:00Z',
        depart_latest: '2099-01-02T12:30:00Z',
        status: 'scheduled',
        route_km: 300,
        origin_label: 'Αλεξανδρούπολη',
        destination_label: 'Θεσσαλονίκη',
        price_eur: 8,
        seats_free: 3,
      },
    ],
  });
  (api.waiting as jest.Mock).mockResolvedValue({
    waiting: [
      {
        request_id: 'req-1',
        passenger_name: 'Ελένη',
        seats: 1,
        luggage: 'none',
        luggage_count: 0,
        window_start: '2099-01-02T12:00:00Z',
        window_end: '2099-01-02T20:59:00Z',
        origin_label: 'Αλεξανδρούπολη',
        destination_label: 'Θεσσαλονίκη',
      },
    ],
  });
  (api.offerSeat as jest.Mock).mockResolvedValue({ ask_id: 'a1' });
});

async function publish() {
  render(wrap(<PublishScreen />));
  fireEvent.changeText(screen.getByTestId('publish-origin-input'), 'αλεξ');
  await waitFor(() => screen.getByTestId('publish-origin-node/Αλεξανδρούπολη'));
  fireEvent.press(screen.getByTestId('publish-origin-node/Αλεξανδρούπολη'));
  fireEvent.changeText(screen.getByTestId('publish-destination-input'), 'θεσ');
  await waitFor(() => screen.getByTestId('publish-destination-node/Θεσσαλονίκη'));
  fireEvent.press(screen.getByTestId('publish-destination-node/Θεσσαλονίκη'));
  await waitFor(() => expect(screen.getByTestId('publish-submit')).toBeEnabled());
  fireEvent.press(screen.getByTestId('publish-submit'));
}

// The driver says yes first: nobody is asked a question the passenger has not
// yet seen the answer to. Publishing is the moment the driver can say it.
test('the confirmation lists the people waiting on a weekend departure of the new route', async () => {
  await publish();

  const title = await screen.findByTestId('waiting-occ-fri-title');
  expect(title).toHaveTextContent(/^1 άτομο περιμένει σε αυτόν τον δρόμο την Παρασκευή\./);
  expect(api.waiting).toHaveBeenCalledWith('occ-fri');
});

test('«Ναι» there offers the seat', async () => {
  await publish();

  fireEvent.press(await screen.findByTestId('waiting-yes-req-1'));

  await waitFor(() => expect(api.offerSeat).toHaveBeenCalledWith('occ-fri', 'req-1'));
  expect(await screen.findByTestId('waiting-offered-req-1')).toBeTruthy();
});

test('nobody waiting adds nothing to the confirmation', async () => {
  (api.waiting as jest.Mock).mockResolvedValue({ waiting: [] });
  await publish();

  await screen.findByTestId('publish-done');
  await waitFor(() => expect(api.waiting).toHaveBeenCalled());
  expect(screen.queryByTestId('waiting-occ-fri')).toBeNull();
});

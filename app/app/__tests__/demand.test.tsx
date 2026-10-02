import { render, screen, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import PublishScreen from '../(tabs)/publish';
import { api } from '../../src/api/endpoints';

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
  // Focus is once, on mount, which is what a test screen gets.
  useFocusEffect: (effect: () => void) => require('react').useEffect(effect, []),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: {
    publishRoute: jest.fn(),
    places: jest.fn(),
    meetingPoints: jest.fn(),
    vehicles: jest.fn(),
    demand: jest.fn(),
  },
  LUGGAGE_SIZES: ['none', 'cabin', 'suitcase', 'bulky'],
}));

jest.mock('../../src/state/session', () => ({
  useSession: (select: (s: unknown) => unknown) => select({ status: 'signedIn' }),
}));

const car = {
  id: 'v1',
  name: 'Toyota Yaris',
  label: '',
  make: 'Toyota',
  model: 'Yaris',
  fuel: 'petrol',
  colour: 'λευκό',
  plate: 'ΑΒΓ-1234',
  consumption_l_100km: 5.6,
  consumption_source: 'dataset' as const,
  consumption_capped: false,
  is_default: true,
};

function wrap(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

beforeEach(() => {
  jest.clearAllMocks();
  (api.vehicles as jest.Mock).mockResolvedValue([car]);
  (api.places as jest.Mock).mockResolvedValue({ places: [] });
  (api.meetingPoints as jest.Mock).mockResolvedValue({ points: [] });
  (api.demand as jest.Mock).mockResolvedValue({ corridors: [], period_days: 7 });
});

// A driver who has published nothing has no reason to publish anything. This is
// the reason, and it was sitting in the requests table the whole time.
test('the publish screen says who is already looking for a lift', async () => {
  (api.demand as jest.Mock).mockResolvedValue({
    corridors: [
      { origin_label: 'Αλεξανδρούπολη', destination_label: 'Ορεστιάδα', people: 3 },
      { origin_label: 'Αλεξανδρούπολη', destination_label: 'Σουφλί', people: 1 },
    ],
    period_days: 7,
  });

  render(wrap(<PublishScreen />));

  const busiest = await screen.findByTestId('demand-0');
  const text = busiest.props.children as string;
  expect(text).toContain('Αλεξανδρούπολη');
  expect(text).toContain('Ορεστιάδα');
  expect(text).toMatch(/3/);

  // One person is one person, not "1 άνθρωποι".
  const quiet = screen.getByTestId('demand-1');
  expect(quiet.props.children as string).toMatch(/1 άνθρωπος/);
});

// Nobody waiting is the ordinary state of a cold-start market. An empty
// heading over nothing would be a worse answer than no heading.
test('with nobody waiting the screen says nothing about demand', async () => {
  render(wrap(<PublishScreen />));

  await waitFor(() => expect(api.demand).toHaveBeenCalled());
  expect(screen.queryByTestId('demand')).toBeNull();
  expect(screen.queryByTestId('demand-0')).toBeNull();
});

// Demand is an encouragement, not part of publishing. A failure to load it must
// not put an error box on a form that works.
test('demand that fails to load is silent', async () => {
  (api.demand as jest.Mock).mockRejectedValue(new Error('offline'));

  render(wrap(<PublishScreen />));

  await waitFor(() => expect(api.demand).toHaveBeenCalled());
  expect(screen.queryByTestId('demand')).toBeNull();
  // The form is still there and still usable.
  expect(screen.getByTestId('publish-submit')).toBeTruthy();
});

// Counts, never names. The component is handed only what the endpoint returns,
// and this asserts it renders nothing else — a future field on the payload
// should not become a line on somebody's screen by accident.
test('a corridor line is a route and a count and nothing else', async () => {
  (api.demand as jest.Mock).mockResolvedValue({
    corridors: [
      {
        origin_label: 'Αλεξανδρούπολη',
        destination_label: 'Ορεστιάδα',
        people: 2,
        // Neither of these should ever be sent, and if one is it must not be
        // drawn.
        passenger_names: ['Γιάννης', 'Μαρία'],
        window_start: '2026-09-17T04:30:00Z',
      },
    ],
    period_days: 7,
  });

  render(wrap(<PublishScreen />));

  const line = await screen.findByTestId('demand-0');
  const text = line.props.children as string;
  expect(text).not.toContain('Γιάννης');
  expect(text).not.toContain('Μαρία');
  expect(text).not.toContain('2026');
  expect(text).not.toContain('04:30');
});

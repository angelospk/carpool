import { render, screen } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import TripScreen from '../trip/[id]';
import { api, type TripDetail } from '../../src/api/endpoints';

jest.mock('expo-router', () => ({
  useLocalSearchParams: () => ({ id: 'occ-1' }),
  useRouter: () => ({ push: jest.fn(), replace: jest.fn(), back: jest.fn(), canGoBack: () => true }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { trip: jest.fn(), messages: jest.fn() },
}));

jest.mock('../../src/components/LiveCard', () => ({ LiveCard: () => null }));
jest.mock('../../src/components/RepeatCard', () => ({ RepeatCard: () => null }));
jest.mock('../../src/components/PendingAsks', () => ({ PendingAsks: () => null }));
// Its own test covers the list; here only whether the trip shows it.
jest.mock('../../src/components/WaitingPassengers', () => {
  const { Text } = require('react-native');
  return {
    WaitingPassengers: ({ occurrenceId }: { occurrenceId: string }) => (
      <Text testID="waiting-for">{occurrenceId}</Text>
    ),
  };
});
jest.mock('../../src/state/session', () => ({
  useSession: (pick: (s: { me: { id: string } }) => unknown) => pick({ me: { id: 'me' } }),
}));

const person = (id: string) => ({
  id,
  name: id,
  photo_url: null,
  rating: null,
  facebook_url: null,
  instagram_url: null,
  can_rate: false,
  rated: false,
});

// The next Athens-noon of a weekday (Friday 5, Monday 1), at least two days
// ahead so the trip is still to come, and near enough that the screen's own
// timers stay within what a timer can hold.
function next(isoDay: number): string {
  const at = new Date();
  at.setUTCHours(12, 0, 0, 0);
  at.setUTCDate(at.getUTCDate() + 2);
  while (((at.getUTCDay() + 6) % 7) + 1 !== isoDay) at.setUTCDate(at.getUTCDate() + 1);
  return at.toISOString();
}

function trip(over: Partial<TripDetail> = {}): TripDetail {
  return {
    id: 'occ-1',
    role: 'driver',
    depart_earliest: next(5),
    depart_latest: next(5),
    status: 'scheduled',
    route_km: 300,
    origin_label: 'Αλεξανδρούπολη',
    destination_label: 'Θεσσαλονίκη',
    price_eur: 8,
    seats_free: 3,
    route_polyline: '',
    driver: person('me'),
    passengers: [],
    template_id: 't-1',
    weekday: 5,
    booking_id: null,
    ends_at_port: false,
    ...over,
  } as TripDetail;
}

function show(t: TripDetail) {
  (api.trip as jest.Mock).mockResolvedValue(t);
  (api.messages as jest.Mock).mockResolvedValue({ messages: [] });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <TripScreen />
    </QueryClientProvider>,
  );
}

// Reachable later than the publish screen: the driver comes back to the trip.
test('a driver sees who waits on a weekend departure', async () => {
  show(trip());

  expect(await screen.findByTestId('waiting-for')).toHaveTextContent('occ-1');
});

test('a weekday departure asks nothing', async () => {
  show(trip({ depart_earliest: next(1), depart_latest: next(1) }));

  await screen.findByTestId('trip-share');
  expect(screen.queryByTestId('waiting-for')).toBeNull();
});

test('a passenger on a weekend trip is not shown the waiting list', async () => {
  show(trip({ role: 'passenger', booking_id: 'bk-1', driver: person('drv'), passengers: [person('me')] }));

  await screen.findByTestId('trip-share');
  expect(screen.queryByTestId('waiting-for')).toBeNull();
});

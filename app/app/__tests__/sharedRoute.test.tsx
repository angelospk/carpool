import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import SharedRouteScreen from '../r/[token]';
import { ApiError } from '../../src/api/client';
import { api } from '../../src/api/endpoints';

const mockPush = jest.fn();
let mockStatus = 'signedIn';

jest.mock('expo-router', () => ({
  useLocalSearchParams: () => ({ token: 'tokABCDEFGHIJKLMNOP' }),
  useRouter: () => ({ push: mockPush, replace: jest.fn() }),
}));
jest.mock('../../src/state/session', () => ({
  useSession: (select: (s: unknown) => unknown) => select({ status: mockStatus }),
}));
jest.mock('../../src/api/endpoints', () => ({
  api: { sharedRoute: jest.fn(), places: jest.fn() },
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const shared = {
  origin_label: 'Αλεξανδρούπολη',
  destination_label: 'Φέρες',
  kind: 'recurring',
  weekdays: [1, 2, 3, 4, 5],
  depart_earliest: '07:30',
  depart_latest: '08:00',
  seats: 3,
  for_group: false,
};

beforeEach(() => {
  jest.clearAllMocks();
  mockStatus = 'signedIn';
});

test('the link shows the same sentence the driver sent', async () => {
  (api.sharedRoute as jest.Mock).mockResolvedValue(shared);
  render(wrap(<SharedRouteScreen />));
  await waitFor(() => expect(screen.getByTestId('shared-route-line')).toBeTruthy());
  expect(screen.getByTestId('shared-route-line').props.children).toBe(
    'Αλεξανδρούπολη → Φέρες · καθημερινές 07:30 · έχω 3 θέσεις',
  );
});

// Searching resolves the names through the public places list, so the link
// never had to carry anybody's coordinates.
test('searching fills the search form with the two places', async () => {
  (api.sharedRoute as jest.Mock).mockResolvedValue(shared);
  (api.places as jest.Mock).mockImplementation(async (q: string) => ({
    places: [{ id: q, name: q, lon: q === 'Φέρες' ? 26.17 : 25.87, lat: 40.9, kind: 'town', area: '' }],
  }));
  render(wrap(<SharedRouteScreen />));
  fireEvent.press(await screen.findByTestId('shared-route-search'));
  await waitFor(() => expect(mockPush).toHaveBeenCalled());
  expect(mockPush.mock.calls[0][0].params).toMatchObject({
    from_label: 'Αλεξανδρούπολη',
    to_label: 'Φέρες',
    to_lon: '26.17',
  });
});

test('somebody without an account is sent to make one', async () => {
  mockStatus = 'signedOut';
  (api.sharedRoute as jest.Mock).mockResolvedValue(shared);
  render(wrap(<SharedRouteScreen />));
  fireEvent.press(await screen.findByTestId('shared-route-join'));
  expect(mockPush).toHaveBeenCalledWith('/(auth)/phone');
});

test('a retired route says so rather than advertising a trip that will not run', async () => {
  (api.sharedRoute as jest.Mock).mockRejectedValue(new ApiError(410, 'route_retired', 'gone'));
  render(wrap(<SharedRouteScreen />));
  await waitFor(() =>
    expect(screen.getByTestId('shared-route-gone').props.children).toBe('Αυτή η διαδρομή δεν προσφέρεται πια.'),
  );
});

// Two villages share a name more often than not in Greece. A name that means
// more than one place fills nothing in rather than the wrong one.
test('a place name that means two places is not guessed', async () => {
  (api.sharedRoute as jest.Mock).mockResolvedValue(shared);
  (api.places as jest.Mock).mockImplementation(async (q: string) => ({
    places:
      q === 'Φέρες'
        ? [{ id: 'a', name: 'Φέρες', lon: 26.17, lat: 40.9 }, { id: 'b', name: 'Φέρες', lon: 22, lat: 39 }]
        : [{ id: 'c', name: q, lon: 25.87, lat: 40.85 }],
  }));
  render(wrap(<SharedRouteScreen />));
  fireEvent.press(await screen.findByTestId('shared-route-search'));
  await waitFor(() => expect(mockPush).toHaveBeenCalledWith('/(tabs)/search'));
});

test('a server that did not answer is not told as a link that does not exist', async () => {
  (api.sharedRoute as jest.Mock).mockRejectedValue(new ApiError(500, 'internal', 'boom'));
  render(wrap(<SharedRouteScreen />));
  await waitFor(() => expect(screen.getByTestId('shared-route-retry')).toBeTruthy());
  expect(screen.queryByTestId('shared-route-gone')).toBeNull();
});

// Somebody who makes an account from the link comes back to it.
test('signing up from the link remembers to come back to it', async () => {
  mockStatus = 'signedOut';
  (api.sharedRoute as jest.Mock).mockResolvedValue(shared);
  render(wrap(<SharedRouteScreen />));
  fireEvent.press(await screen.findByTestId('shared-route-join'));
  expect(require('../../src/state/returnTo').takeReturn()).toBe('/r/tokABCDEFGHIJKLMNOP');
});

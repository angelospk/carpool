import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { Alert } from 'react-native';

import MyRoutesScreen from '../profile/routes';
import { api } from '../../src/api/endpoints';

const mockPush = jest.fn();
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush, replace: jest.fn() }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { myRoutes: jest.fn(), retireRoute: jest.fn(), shareRouteLink: jest.fn(), setRouteNudge: jest.fn() },
}));

const route = (over: Partial<Record<string, unknown>> = {}) => ({
  id: 'r1',
  origin_label: 'Σουφλί',
  destination_label: 'Αλεξανδρούπολη',
  route_km: 62.4,
  seats: 3,
  pricing_mode: 'formula',
  kind: 'recurring',
  weekdays: [1, 2, 3, 4, 5],
  depart_earliest: '07:30',
  depart_latest: '08:00',
  upcoming_trips: 14,
  kept_trips: 0,
  origin_lon: 26.3,
  origin_lat: 41.19,
  destination_lon: 25.87,
  destination_lat: 40.84,
  ...over,
});

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

let alert: jest.SpyInstance;
beforeEach(() => {
  alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
});
afterEach(() => {
  alert.mockRestore();
  jest.clearAllMocks();
});

const messageWhenRetiring = async () => {
  render(wrap(<MyRoutesScreen />));
  fireEvent.press(await screen.findByTestId('retire-r1'));
  await waitFor(() => expect(alert).toHaveBeenCalled());
  return alert.mock.calls[0][1] as string;
};

// Retiring calls off the empty departures with the route. The driver used to be
// told every scheduled trip stayed and had to be cancelled by hand — fourteen
// of them for a weekday route.
test('a route nobody is booked on says its trips go with it', async () => {
  (api.myRoutes as jest.Mock).mockResolvedValue({ routes: [route()] });
  const message = await messageWhenRetiring();
  expect(message).toContain('ακυρώνονται');
  expect(message).not.toContain('14');
});

test('a route with passengers says how many of its trips stay', async () => {
  (api.myRoutes as jest.Mock).mockResolvedValue({ routes: [route({ kept_trips: 2 })] });
  const message = await messageWhenRetiring();
  expect(message).toContain('Μένουν 2 με επιβάτες');
});

// «Αλλαγή» opens the publish form with this route in it, marked as the one
// being replaced.
test('changing a route opens the form filled in with it', async () => {
  (api.myRoutes as jest.Mock).mockResolvedValue({ routes: [route()] });
  render(wrap(<MyRoutesScreen />));

  fireEvent.press(await screen.findByTestId('edit-r1'));

  expect(mockPush).toHaveBeenCalledWith({
    pathname: '/(tabs)/publish',
    params: expect.objectContaining({
      replaces: 'r1',
      from_label: 'Σουφλί',
      from_lon: '26.3',
      to_label: 'Αλεξανδρούπολη',
      weekdays: '1,2,3,4,5',
      earliest: '07:30',
      seats: '3',
    }),
  });
});

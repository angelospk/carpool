import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { Share } from 'react-native';

import MyRoutesScreen from '../profile/routes';
import { api } from '../../src/api/endpoints';

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { myRoutes: jest.fn(), retireRoute: jest.fn() },
}));

const route = (over: Partial<Record<string, unknown>> = {}) => ({
  id: 'r1',
  origin_label: 'Αλεξανδρούπολη',
  destination_label: 'Σουφλί',
  route_km: 62.4,
  seats: 2,
  pricing_mode: 'formula',
  kind: 'recurring',
  weekdays: [1, 2, 3, 4, 5],
  depart_earliest: '07:30',
  depart_latest: '08:00',
  upcoming_trips: 0,
  ...over,
});

function wrap(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

let share: jest.SpyInstance;

beforeEach(() => {
  jest.clearAllMocks();
  share = jest.spyOn(Share, 'share').mockResolvedValue({ action: 'sharedAction' });
});

afterEach(() => {
  share.mockRestore();
});

// The driver publishes the morning and then nothing happens. This is the one
// path out of the app: the sentence goes into whichever group chat the
// passengers are already in.
test('a published route can be handed to the share sheet', async () => {
  (api.myRoutes as jest.Mock).mockResolvedValue({ routes: [route()] });

  render(wrap(<MyRoutesScreen />));

  fireEvent.press(await screen.findByTestId('share-r1'));

  await waitFor(() => expect(share).toHaveBeenCalledTimes(1));
  const message = share.mock.calls[0][0].message as string;
  expect(message).toContain('Αλεξανδρούπολη → Σουφλί');
  expect(message).toContain('καθημερινές 07:30');
  expect(message).toMatch(/2 θέσεις/);
});

// Opening the sheet and closing it again is not a failure, and saying so would
// tell the driver something went wrong when nothing did.
test('closing the sheet without sending says nothing', async () => {
  (api.myRoutes as jest.Mock).mockResolvedValue({ routes: [route()] });
  share.mockResolvedValue({ action: 'dismissedAction' });

  render(wrap(<MyRoutesScreen />));

  fireEvent.press(await screen.findByTestId('share-r1'));

  await waitFor(() => expect(share).toHaveBeenCalledTimes(1));
  expect(screen.queryByTestId('share-failed-r1')).toBeNull();
});

// The native sheet rejects on a real failure. An unhandled rejection here
// takes the screen down, and silence leaves the driver pressing a dead button.
test('a share sheet that fails says so and leaves the list standing', async () => {
  (api.myRoutes as jest.Mock).mockResolvedValue({ routes: [route()] });
  share.mockRejectedValue(new Error('no activity'));

  render(wrap(<MyRoutesScreen />));

  fireEvent.press(await screen.findByTestId('share-r1'));

  expect(await screen.findByTestId('share-failed-r1')).toBeTruthy();
  expect(screen.getByTestId('route-r1')).toBeTruthy();
});

// Nothing to share until something is published.
test('an empty list offers nothing to share', async () => {
  (api.myRoutes as jest.Mock).mockResolvedValue({ routes: [] });

  render(wrap(<MyRoutesScreen />));

  await waitFor(() => expect(api.myRoutes).toHaveBeenCalled());
  expect(screen.queryByTestId('share-r1')).toBeNull();
});

// Each card shares its own route. Two routes and one shared message would be
// the kind of bug nobody notices until somebody drives to the wrong town.
test('two routes share two different messages', async () => {
  (api.myRoutes as jest.Mock).mockResolvedValue({
    routes: [
      route(),
      route({ id: 'r2', destination_label: 'Κομοτηνή', weekdays: [6, 7], depart_earliest: '09:00' }),
    ],
  });

  render(wrap(<MyRoutesScreen />));

  fireEvent.press(await screen.findByTestId('share-r2'));

  await waitFor(() => expect(share).toHaveBeenCalledTimes(1));
  const message = share.mock.calls[0][0].message as string;
  expect(message).toContain('Κομοτηνή');
  expect(message).toContain('09:00');
  expect(message).not.toContain('Σουφλί');
});

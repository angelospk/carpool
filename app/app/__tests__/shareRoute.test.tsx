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
  api: { myRoutes: jest.fn(), retireRoute: jest.fn(), shareRouteLink: jest.fn() },
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
  // No link by default, so the sentence tests below see the sentence alone.
  (api.shareRouteLink as jest.Mock).mockRejectedValue(new Error('offline'));
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

// Two presses inside one tick. `setSharing(true)` does not take effect until
// React re-renders, so a state flag alone lets both presses through and two
// native sheets are queued. The guard has to be a ref, set before the await.
//
// The share promise is held open deliberately: that is the window in which the
// second press lands, and resolving it first would test nothing.
test('a second press while the sheet is open is ignored', async () => {
  (api.myRoutes as jest.Mock).mockResolvedValue({ routes: [route()] });
  let release: (v: unknown) => void = () => {};
  share.mockReturnValue(new Promise((r) => (release = r)));

  render(wrap(<MyRoutesScreen />));
  const button = await screen.findByTestId('share-r1');

  fireEvent.press(button);
  fireEvent.press(button);
  fireEvent.press(button);

  // Awaited: the sheet opens after the link is asked for, so it is no longer
  // on the same tick as the press. Three presses are still one sheet.
  await waitFor(() => expect(share).toHaveBeenCalledTimes(1));
  await new Promise((r) => setTimeout(r, 20));
  expect(share).toHaveBeenCalledTimes(1);

  // Once it closes, sharing again is allowed.
  release({ action: 'sharedAction' });
  await waitFor(() => expect(share).toHaveBeenCalledTimes(1));
  fireEvent.press(button);
  await waitFor(() => expect(share).toHaveBeenCalledTimes(2));
});

// A sheet that failed must not leave the button dead. The ref has to be
// released on the error path as surely as on the happy one.
test('a failed share can be retried', async () => {
  (api.myRoutes as jest.Mock).mockResolvedValue({ routes: [route()] });
  share.mockRejectedValue(new Error('no activity'));

  render(wrap(<MyRoutesScreen />));
  const button = await screen.findByTestId('share-r1');

  fireEvent.press(button);
  expect(await screen.findByTestId('share-failed-r1')).toBeTruthy();

  share.mockResolvedValue({ action: 'sharedAction' });
  fireEvent.press(button);

  await waitFor(() => expect(share).toHaveBeenCalledTimes(2));
  // The hint from the failed attempt does not survive a successful one.
  await waitFor(() => expect(screen.queryByTestId('share-failed-r1')).toBeNull());
});

// With a link, the link rides along on its own line after the sentence; the
// sentence is unchanged, because a chat may not preview it.
test('the message carries the route link when there is one', async () => {
  (api.myRoutes as jest.Mock).mockResolvedValue({ routes: [route()] });
  (api.shareRouteLink as jest.Mock).mockResolvedValue({ token: 'tok', url: 'https://carpool.test/r/tok' });
  render(wrap(<MyRoutesScreen />));
  const button = await screen.findByTestId('share-r1');
  fireEvent.press(button);
  await waitFor(() => expect(share).toHaveBeenCalled());
  const message: string = share.mock.calls[0][0].message;
  expect(message.split('\n').pop()).toBe('https://carpool.test/r/tok');
  expect(api.shareRouteLink).toHaveBeenCalledWith('r1');
});

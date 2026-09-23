import { render, screen, fireEvent, waitFor, act } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import * as Location from 'expo-location';

import { LiveCard } from '../LiveCard';
import { ApiError } from '../../api/client';
import { api } from '../../api/endpoints';

jest.mock('../../api/endpoints', () => ({
  api: { live: jest.fn(), putPosition: jest.fn(), stopPosition: jest.fn() },
}));

let mockOnPosition: ((p: { coords: { latitude: number; longitude: number } }) => void) | null = null;
const mockRemove = jest.fn();
jest.mock('expo-location', () => ({
  Accuracy: { Balanced: 3 },
  requestForegroundPermissionsAsync: jest.fn(),
  watchPositionAsync: jest.fn(async (_opts: unknown, cb: typeof mockOnPosition) => {
    mockOnPosition = cb;
    return { remove: mockRemove };
  }),
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const at = new Date('2026-07-30T04:20:00Z').getTime();
const window = { opens: '2026-07-30T04:00:00Z', closes: '2026-07-30T06:20:00Z' };

beforeEach(() => {
  jest.clearAllMocks();
  mockOnPosition = null;
  (api.putPosition as jest.Mock).mockResolvedValue(undefined);
  (api.stopPosition as jest.Mock).mockResolvedValue(undefined);
  (Location.requestForegroundPermissionsAsync as jest.Mock).mockResolvedValue({ granted: true });
});

test('shows the driver and how long ago they were seen, with a map link', async () => {
  (api.live as jest.Mock).mockResolvedValue({
    ...window,
    positions: [{ user_id: 'd', name: 'Γιώργος', lat: 40.85, lon: 25.87, updated_at: '2026-07-30T04:18:00Z' }],
  });
  render(wrap(<LiveCard tripId="occ-1" now={() => at} />));
  await waitFor(() => expect(screen.getByTestId('live-d')).toBeTruthy());
  expect(screen.getByText('Γιώργος · πριν 2′')).toBeTruthy();
});

// Before the window there is nothing to press, and it says when there will be.
test('before the trip it says when sharing opens and offers nothing', async () => {
  (api.live as jest.Mock).mockResolvedValue({ ...window, positions: [] });
  render(wrap(<LiveCard tripId="occ-1" now={() => new Date('2026-07-29T12:00:00Z').getTime()} />));
  await waitFor(() => expect(screen.getByTestId('live-not-yet')).toBeTruthy());
  expect(screen.queryByTestId('live-start')).toBeNull();
});

test('sharing sends positions, and stopping forgets them on the server', async () => {
  (api.live as jest.Mock).mockResolvedValue({ ...window, positions: [] });
  render(wrap(<LiveCard tripId="occ-1" now={() => at} />));
  await waitFor(() => expect(screen.getByTestId('live-start')).toBeTruthy());

  fireEvent.press(screen.getByTestId('live-start'));
  await waitFor(() => expect(mockOnPosition).not.toBeNull());
  act(() => mockOnPosition!({ coords: { latitude: 40.85, longitude: 25.87 } }));
  expect(api.putPosition).toHaveBeenCalledWith('occ-1', 40.85, 25.87);

  fireEvent.press(screen.getByTestId('live-stop'));
  await waitFor(() => expect(api.stopPosition).toHaveBeenCalledWith('occ-1'));
  expect(mockRemove).toHaveBeenCalled();
});

// When the server says the window closed, the phone stops sending into a
// refusal every fifteen seconds.
test('a refusal from the server stops the sharing', async () => {
  (api.live as jest.Mock).mockResolvedValue({ ...window, positions: [] });
  (api.putPosition as jest.Mock).mockRejectedValue(new ApiError(409, 'not_sharing_now', 'no'));
  render(wrap(<LiveCard tripId="occ-1" now={() => at} />));
  await waitFor(() => expect(screen.getByTestId('live-start')).toBeTruthy());
  fireEvent.press(screen.getByTestId('live-start'));
  await waitFor(() => expect(mockOnPosition).not.toBeNull());
  act(() => mockOnPosition!({ coords: { latitude: 40.85, longitude: 25.87 } }));
  await waitFor(() => expect(mockRemove).toHaveBeenCalled());
  await waitFor(() => expect(screen.getByTestId('live-start')).toBeTruthy());
});

test('without permission it says so and sends nothing', async () => {
  (api.live as jest.Mock).mockResolvedValue({ ...window, positions: [] });
  (Location.requestForegroundPermissionsAsync as jest.Mock).mockResolvedValue({ granted: false });
  render(wrap(<LiveCard tripId="occ-1" now={() => at} />));
  await waitFor(() => expect(screen.getByTestId('live-start')).toBeTruthy());
  fireEvent.press(screen.getByTestId('live-start'));
  await waitFor(() => expect(screen.getByTestId('live-error')).toBeTruthy());
  expect(Location.watchPositionAsync).not.toHaveBeenCalled();
});

// QA found this: a PUT that left before the screen closed landed after the
// DELETE and put the position back on the server for up to an hour.
test('stopping waits for the last upload before forgetting the position', async () => {
  (api.live as jest.Mock).mockResolvedValue({ ...window, positions: [] });
  let land: () => void = () => {};
  (api.putPosition as jest.Mock).mockReturnValue(new Promise<void>((r) => (land = r)));
  const order: string[] = [];
  (api.stopPosition as jest.Mock).mockImplementation(async () => void order.push('delete'));

  render(wrap(<LiveCard tripId="occ-1" now={() => at} />));
  fireEvent.press(await screen.findByTestId('live-start'));
  await waitFor(() => expect(mockOnPosition).not.toBeNull());
  act(() => mockOnPosition!({ coords: { latitude: 40.85, longitude: 25.87 } }));

  fireEvent.press(await screen.findByTestId('live-stop'));
  await new Promise((r) => setTimeout(r, 20));
  expect(order).toEqual([]);
  order.push('put');
  land();
  await waitFor(() => expect(order).toEqual(['put', 'delete']));

  // And a position that arrives after stopping is not sent at all.
  (api.putPosition as jest.Mock).mockClear();
  act(() => mockOnPosition!({ coords: { latitude: 41, longitude: 26 } }));
  expect(api.putPosition).not.toHaveBeenCalled();
});

test('under a minute old reads as now', async () => {
  (api.live as jest.Mock).mockResolvedValue({
    ...window,
    positions: [{ user_id: 'd', name: 'Γιώργος', lat: 40.85, lon: 25.87, updated_at: '2026-07-30T04:19:50Z' }],
  });
  render(wrap(<LiveCard tripId="occ-1" now={() => at} />));
  expect(await screen.findByText('Γιώργος · τώρα')).toBeTruthy();
});

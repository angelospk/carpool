import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { Linking } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import AskScreen from '../ask/[id]';
import { ApiError } from '../../src/api/client';
import { api } from '../../src/api/endpoints';

// jest hoists jest.mock above these, so the names must start with "mock" —
// that is the only prefix jest allows a factory to reference.
const mockParams: Record<string, string> = { id: 'ask-1' };

const mockReplace = jest.fn();

jest.mock('expo-router', () => ({
  useLocalSearchParams: () => mockParams,
  useRouter: () => ({ replace: mockReplace, push: jest.fn() }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { acceptAsk: jest.fn(), declineAsk: jest.fn(), ask: jest.fn(), trip: jest.fn() },
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const theAsk = {
  id: 'ask-1',
  occurrence_id: 'occ-1',
  passenger_name: 'Ελένη',
  origin_label: 'Αλεξανδρούπολη',
  destination_label: 'Φέρες',
  detour_min: 6,
  detour_km: 2.1,
  price_eur: 3.5,
  seats: 1,
  depart_earliest: '2026-10-01T04:30:00Z',
  expires_at: '2026-10-01T04:00:00Z',
};

beforeEach(() => {
  jest.clearAllMocks();
  delete mockParams.take;
  (api.ask as jest.Mock).mockResolvedValue(theAsk);
});

// The driver has to decide in one glance, so the detour has to be on screen.
test('shows the detour the driver is being asked to make', async () => {
  render(wrap(<AskScreen />));

  const detour = String((await screen.findByTestId('ask-detour')).props.children);
  expect(detour).toContain('6');
  expect(detour).toContain('2.1');
});

test('accepting sends the accept and returns to the trip list', async () => {
  (api.acceptAsk as jest.Mock).mockResolvedValue({ booking_id: 'b1' });

  render(wrap(<AskScreen />));
  fireEvent.press(await screen.findByTestId('ask-accept'));

  await waitFor(() => expect(api.acceptAsk).toHaveBeenCalledWith('ask-1'));
  await waitFor(() => expect(mockReplace).toHaveBeenCalledWith('/(tabs)'));
});

test('declining sends the decline', async () => {
  (api.declineAsk as jest.Mock).mockResolvedValue(undefined);

  render(wrap(<AskScreen />));
  fireEvent.press(await screen.findByTestId('ask-decline'));

  await waitFor(() => expect(api.declineAsk).toHaveBeenCalledWith('ask-1'));
});

// A push can be minutes old by the time it is tapped, so a stale ask is the
// normal case rather than an error. Each reason gets its own message.
test('a stale tap on an expired ask explains what happened', async () => {
  (api.acceptAsk as jest.Mock).mockRejectedValue(
    new ApiError(409, 'ask_expired', 'this request has expired'),
  );

  render(wrap(<AskScreen />));
  fireEvent.press(await screen.findByTestId('ask-accept'));

  await waitFor(() => expect(screen.getByTestId('ask-outcome')).toBeTruthy());
  expect(mockReplace).not.toHaveBeenCalled();
});

test('a trip that filled up says so rather than failing generically', async () => {
  (api.acceptAsk as jest.Mock).mockRejectedValue(
    new ApiError(409, 'no_seats', 'the trip has no free seats left'),
  );

  render(wrap(<AskScreen />));
  fireEvent.press(await screen.findByTestId('ask-accept'));

  await waitFor(() => expect(screen.getByTestId('ask-outcome')).toBeTruthy());
});

// Once answered, the buttons must not fire a second request.
test('the buttons are disabled after the ask is answered', async () => {
  (api.acceptAsk as jest.Mock).mockRejectedValue(
    new ApiError(409, 'ask_closed', 'already answered'),
  );

  render(wrap(<AskScreen />));
  fireEvent.press(await screen.findByTestId('ask-accept'));
  await waitFor(() => expect(screen.getByTestId('ask-outcome')).toBeTruthy());

  // Nothing left to press but the way out.
  expect(screen.queryByTestId('ask-accept')).toBeNull();
  expect(screen.getByTestId('ask-leave')).toBeTruthy();
  expect(api.acceptAsk).toHaveBeenCalledTimes(1);
});

// A notification opens this screen with only an id. The driver has to see who
// is asking and for which journey, not a detour of zero minutes.
test('opened by id, it says who is asking and for which journey', async () => {
  render(wrap(<AskScreen />));

  expect(await screen.findByText(/Ελένη/)).toBeTruthy();
  expect(screen.getByText('Αλεξανδρούπολη')).toBeTruthy();
  expect(screen.getByText('Φέρες')).toBeTruthy();
  expect(api.ask).toHaveBeenCalledWith('ask-1');
});

// Tapped after somebody else answered it, or after it lapsed: the server no
// longer shows it, and the screen says so instead of offering two buttons.
test('an ask that is gone says so and offers the way back', async () => {
  (api.ask as jest.Mock).mockRejectedValue(new ApiError(404, 'not_found', 'no such request'));

  render(wrap(<AskScreen />));

  expect(await screen.findByTestId('ask-gone')).toBeTruthy();
  expect(screen.queryByTestId('ask-accept')).toBeNull();
  fireEvent.press(screen.getByTestId('ask-leave'));
  expect(mockReplace).toHaveBeenCalledWith('/(tabs)');
});

// A failure that may work on a second press leaves the two buttons there.
test('a network failure keeps the answer open', async () => {
  (api.acceptAsk as jest.Mock).mockRejectedValue(new Error('offline'));

  render(wrap(<AskScreen />));
  fireEvent.press(await screen.findByTestId('ask-accept'));

  await waitFor(() => expect(screen.getByTestId('ask-outcome')).toBeTruthy());
  expect(screen.getByTestId('ask-accept')).toBeTruthy();
  expect(screen.queryByTestId('ask-leave')).toBeNull();
});

// «Πάρ' τον» on the notification, in a car: the offer is accepted without
// another tap, the app is left on the trip, and the new pickup goes to the
// maps app as a stop on the way to the end of the road.
test('«Πάρ\' τον» accepts and hands the pickup to the maps app', async () => {
  mockParams.take = '1';
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(true);
  (api.acceptAsk as jest.Mock).mockResolvedValue({ booking_id: 'b1' });
  (api.trip as jest.Mock).mockResolvedValue({
    destination: [26.0, 40.86],
    passengers: [
      { booking_id: 'b0', pickup: [25.85, 40.85] },
      { booking_id: 'b1', pickup: [25.9, 40.85] },
    ],
  });
  render(wrap(<AskScreen />));

  await waitFor(() => expect(api.acceptAsk).toHaveBeenCalledWith('ask-1'));
  await waitFor(() => expect(open).toHaveBeenCalled());
  expect(open.mock.calls[0][0]).toContain('40.85%2C25.9');
  expect(open.mock.calls[0][0]).toContain('40.86%2C26');
  expect(mockReplace).toHaveBeenCalledWith({ pathname: '/trip/[id]', params: { id: 'occ-1' } });
  expect(api.acceptAsk).toHaveBeenCalledTimes(1);
});

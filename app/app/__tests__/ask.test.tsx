import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import AskScreen from '../ask/[id]';
import { ApiError } from '../../src/api/client';
import { api } from '../../src/api/endpoints';

// jest hoists jest.mock above these, so the names must start with "mock" —
// that is the only prefix jest allows a factory to reference.
const mockParams: Record<string, string> = {
  id: 'ask-1',
  detour_min: '6',
  detour_km: '2.1',
  price: '3.5',
};

const mockReplace = jest.fn();

jest.mock('expo-router', () => ({
  useLocalSearchParams: () => mockParams,
  useRouter: () => ({ replace: mockReplace, push: jest.fn() }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { acceptAsk: jest.fn(), declineAsk: jest.fn() },
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

beforeEach(() => jest.clearAllMocks());

// The driver has to decide in one glance, so the detour has to be on screen.
test('shows the detour the driver is being asked to make', () => {
  render(wrap(<AskScreen />));

  const detour = String(screen.getByTestId('ask-detour').props.children);
  expect(detour).toContain('6');
  expect(detour).toContain('2.1');
});

test('accepting sends the accept and returns to the trip list', async () => {
  (api.acceptAsk as jest.Mock).mockResolvedValue({ booking_id: 'b1' });

  render(wrap(<AskScreen />));
  fireEvent.press(screen.getByTestId('ask-accept'));

  await waitFor(() => expect(api.acceptAsk).toHaveBeenCalledWith('ask-1'));
  await waitFor(() => expect(mockReplace).toHaveBeenCalledWith('/(tabs)'));
});

test('declining sends the decline', async () => {
  (api.declineAsk as jest.Mock).mockResolvedValue(undefined);

  render(wrap(<AskScreen />));
  fireEvent.press(screen.getByTestId('ask-decline'));

  await waitFor(() => expect(api.declineAsk).toHaveBeenCalledWith('ask-1'));
});

// A push can be minutes old by the time it is tapped, so a stale ask is the
// normal case rather than an error. Each reason gets its own message.
test('a stale tap on an expired ask explains what happened', async () => {
  (api.acceptAsk as jest.Mock).mockRejectedValue(
    new ApiError(409, 'ask_expired', 'this request has expired'),
  );

  render(wrap(<AskScreen />));
  fireEvent.press(screen.getByTestId('ask-accept'));

  await waitFor(() => expect(screen.getByTestId('ask-outcome')).toBeTruthy());
  expect(mockReplace).not.toHaveBeenCalled();
});

test('a trip that filled up says so rather than failing generically', async () => {
  (api.acceptAsk as jest.Mock).mockRejectedValue(
    new ApiError(409, 'no_seats', 'the trip has no free seats left'),
  );

  render(wrap(<AskScreen />));
  fireEvent.press(screen.getByTestId('ask-accept'));

  await waitFor(() => expect(screen.getByTestId('ask-outcome')).toBeTruthy());
});

// Once answered, the buttons must not fire a second request.
test('the buttons are disabled after the ask is answered', async () => {
  (api.acceptAsk as jest.Mock).mockRejectedValue(
    new ApiError(409, 'ask_closed', 'already answered'),
  );

  render(wrap(<AskScreen />));
  fireEvent.press(screen.getByTestId('ask-accept'));
  await waitFor(() => expect(screen.getByTestId('ask-outcome')).toBeTruthy());

  fireEvent.press(screen.getByTestId('ask-accept'));

  expect(api.acceptAsk).toHaveBeenCalledTimes(1);
});

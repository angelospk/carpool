import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import RequestScreen from '../request/[id]';
import { ApiError } from '../../src/api/client';
import { api } from '../../src/api/endpoints';

const mockReplace = jest.fn();

jest.mock('expo-router', () => ({
  useLocalSearchParams: () => ({ id: 'req-1' }),
  useRouter: () => ({ replace: mockReplace, push: jest.fn() }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { cancelRequest: jest.fn() },
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

beforeEach(() => jest.clearAllMocks());

test('cancelling a request that is still asking takes them home', async () => {
  (api.cancelRequest as jest.Mock).mockResolvedValue(undefined);

  render(wrap(<RequestScreen />));
  fireEvent.press(screen.getByTestId('cancel-request'));

  await waitFor(() => expect(mockReplace).toHaveBeenCalled());
});

// This screen is where somebody sits while drivers are asked, so a driver
// accepting *while it is open* is the ordinary case rather than a rare race.
// Leaving is the one thing that must not happen: it would tell them they are
// not going while a seat is held in their name and a driver is on the way.
test('a driver taking the ride keeps them on the screen and says so', async () => {
  (api.cancelRequest as jest.Mock).mockRejectedValue(
    new ApiError(409, 'already_matched', 'a driver has taken this ride'),
  );

  render(wrap(<RequestScreen />));
  fireEvent.press(screen.getByTestId('cancel-request'));

  await waitFor(() => expect(screen.getByTestId('request-outcome')).toBeTruthy());
  expect(mockReplace).not.toHaveBeenCalled();
});

test('a failure to cancel is not read as a cancellation', async () => {
  (api.cancelRequest as jest.Mock).mockRejectedValue(new ApiError(500, 'internal', 'boom'));

  render(wrap(<RequestScreen />));
  fireEvent.press(screen.getByTestId('cancel-request'));

  await waitFor(() => expect(screen.getByTestId('request-outcome')).toBeTruthy());
  expect(mockReplace).not.toHaveBeenCalled();
});

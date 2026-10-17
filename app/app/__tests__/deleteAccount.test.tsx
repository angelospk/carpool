import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import DeleteAccountScreen from '../profile/deleteAccount';
import { api } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';

jest.mock('../../src/api/endpoints', () => ({
  api: { requestDeleteCode: jest.fn(), deleteAccount: jest.fn() },
}));

jest.mock('../../src/api/client', () => {
  const actual = jest.requireActual('../../src/api/client');
  return {
    ApiError: actual.ApiError,
    NetworkError: actual.NetworkError,
    currentBaseUrl: () => 'http://localhost:8080',
  };
});

const mockSignOut = jest.fn(async () => {});
jest.mock('../../src/state/session', () => {
  const state = { signOut: () => mockSignOut() };
  const useSession = (select: (s: typeof state) => unknown) => select(state);
  useSession.getState = () => state;
  return { useSession };
});

const open = () =>
  render(
    <QueryClientProvider client={new QueryClient()}>
      <DeleteAccountScreen />
    </QueryClientProvider>,
  );

// QA A14: deleting the account needed only the access token. It now needs the
// code the server texts to the account's own number.
describe('deleting the account', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    (api.requestDeleteCode as jest.Mock).mockResolvedValue(undefined);
  });

  test('asks for a code on arrival and says it was sent', async () => {
    open();
    await waitFor(() => expect(api.requestDeleteCode).toHaveBeenCalledTimes(1));
    expect(screen.getByText('Σου στείλαμε κωδικό με SMS')).toBeTruthy();
    expect(api.deleteAccount).not.toHaveBeenCalled();
  });

  test('cannot be sent without six digits', async () => {
    open();
    fireEvent.changeText(screen.getByTestId('delete-code'), '123');
    fireEvent.press(screen.getByTestId('delete-confirm'));
    expect(api.deleteAccount).not.toHaveBeenCalled();
  });

  test('sends the code, then signs out', async () => {
    (api.deleteAccount as jest.Mock).mockResolvedValue(undefined);
    open();
    fireEvent.changeText(screen.getByTestId('delete-code'), '123456');
    fireEvent.press(screen.getByTestId('delete-confirm'));

    await waitFor(() => expect(api.deleteAccount).toHaveBeenCalledWith('123456'));
    await waitFor(() => expect(mockSignOut).toHaveBeenCalledTimes(1));
  });

  test('a wrong code says so and does not sign out', async () => {
    (api.deleteAccount as jest.Mock).mockRejectedValue(
      new ApiError(403, 'bad_code', 'that code is not valid or has expired'),
    );
    open();
    fireEvent.changeText(screen.getByTestId('delete-code'), '000000');
    fireEvent.press(screen.getByTestId('delete-confirm'));

    await waitFor(() => expect(screen.getByText('Λάθος ή ληγμένος κωδικός.')).toBeTruthy());
    expect(mockSignOut).not.toHaveBeenCalled();
  });
});

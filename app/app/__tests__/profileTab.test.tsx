import { render, screen, fireEvent } from '@testing-library/react-native';

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import ProfileScreen from '../(tabs)/profile';
import { confirm } from '../../src/components/confirm';
import el from '../../src/i18n/el.json';

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
}));

jest.mock('../../src/api/endpoints', () => ({
  ...jest.requireActual('../../src/api/endpoints'),
  api: {},
}));
jest.mock('../../src/components/Settings', () => ({ Settings: () => null }));
jest.mock('../../src/components/confirm', () => ({ confirm: jest.fn() }));

const mockSignOut = jest.fn(async () => {});
jest.mock('../../src/state/session', () => {
  const state = { me: null, setMe: jest.fn(), signOut: () => mockSignOut() };
  const useSession = (select: (s: typeof state) => unknown) => select(state);
  useSession.getState = () => state;
  return { useSession };
});

beforeEach(() => jest.clearAllMocks());

const screenUnderTest = () => (
  <QueryClientProvider client={new QueryClient()}>
    <ProfileScreen />
  </QueryClientProvider>
);

// QA F16: one stray tap on the last button of the screen ended the session, and
// getting back in costs an SMS.
describe('signing out', () => {
  test('asks first, and does not sign out on the tap alone', () => {
    render(screenUnderTest());
    fireEvent.press(screen.getByTestId('sign-out'));

    expect(mockSignOut).not.toHaveBeenCalled();
    expect(confirm).toHaveBeenCalledTimes(1);
    const opts = (confirm as jest.Mock).mock.calls[0][0];
    expect(opts.message).toBe('Θα χρειαστείς νέο κωδικό SMS για να ξαναμπείς.');
    expect(opts.confirmLabel).toBe(el.profile.signOut);
    expect(opts.destructive).toBe(true);
  });

  test('signs out once the question is answered yes', () => {
    render(screenUnderTest());
    fireEvent.press(screen.getByTestId('sign-out'));
    (confirm as jest.Mock).mock.calls[0][0].onConfirm();

    expect(mockSignOut).toHaveBeenCalledTimes(1);
  });
});

import { renderHook, waitFor } from '@testing-library/react-native';

import { usePushRegistration } from '../usePushRegistration';
import { usePushReach } from '../reach';
import { register } from '../register';

jest.mock('../register', () => ({ register: jest.fn() }));
jest.mock('expo-notifications', () => ({}));
jest.mock('expo-device', () => ({ isDevice: true }));
jest.mock('../../api/endpoints', () => ({ api: {} }));

afterEach(() => usePushReach.setState({ on: false }));

test('a registered token turns the promise of a notification on', async () => {
  (register as jest.Mock).mockResolvedValue({ done: true, token: 't' });
  renderHook(() => usePushRegistration(true));
  await waitFor(() => expect(usePushReach.getState().on).toBe(true));
});

// The next account on this phone has not registered anything yet. Until it
// has, it must not be told a notification is coming.
test('signing out takes it back', async () => {
  (register as jest.Mock).mockResolvedValue({ done: true, token: 't' });
  const { rerender } = renderHook(({ signedIn }: { signedIn: boolean }) => usePushRegistration(signedIn), {
    initialProps: { signedIn: true },
  });
  await waitFor(() => expect(usePushReach.getState().on).toBe(true));

  rerender({ signedIn: false });
  expect(usePushReach.getState().on).toBe(false);
});

test('a new registration starts from off, not from the last account', async () => {
  usePushReach.setState({ on: true });
  (register as jest.Mock).mockReturnValue(new Promise(() => {}));
  renderHook(() => usePushRegistration(true));
  expect(usePushReach.getState().on).toBe(false);
});

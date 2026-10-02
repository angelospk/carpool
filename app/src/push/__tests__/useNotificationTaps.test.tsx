import { renderHook } from '@testing-library/react-native';
import * as Notifications from 'expo-notifications';

import { useNotificationTaps } from '../useNotificationTaps';

const mockPush = jest.fn();
jest.mock('expo-router', () => ({ useRouter: () => ({ push: mockPush }) }));

let mockResponse: unknown = null;
jest.mock('expo-notifications', () => ({
  DEFAULT_ACTION_IDENTIFIER: 'default',
  useLastNotificationResponse: () => mockResponse,
  clearLastNotificationResponse: jest.fn(),
}));

const tap = (id: string, data: Record<string, unknown>) => ({
  actionIdentifier: 'default',
  notification: { request: { identifier: id, content: { data } } },
});

beforeEach(() => {
  jest.clearAllMocks();
  mockResponse = null;
});

test('a tap on an ask opens the ask, once, and is cleared', () => {
  mockResponse = tap('n-1', { ask_id: 'a-1' });
  const { rerender } = renderHook(({ signedIn }: { signedIn: boolean }) => useNotificationTaps(signedIn), {
    initialProps: { signedIn: true },
  });
  rerender({ signedIn: true });

  expect(mockPush).toHaveBeenCalledTimes(1);
  expect(mockPush).toHaveBeenCalledWith('/ask/a-1');
  expect(Notifications.clearLastNotificationResponse).toHaveBeenCalledTimes(1);
});

// A tap that launched the app arrives before sign-in has finished. It waits
// rather than being thrown away.
test('a tap before sign-in is acted on once signed in', () => {
  mockResponse = tap('n-2', { ask_id: 'a-2' });
  const { rerender } = renderHook(({ signedIn }: { signedIn: boolean }) => useNotificationTaps(signedIn), {
    initialProps: { signedIn: false },
  });
  expect(mockPush).not.toHaveBeenCalled();
  expect(Notifications.clearLastNotificationResponse).not.toHaveBeenCalled();

  rerender({ signedIn: true });
  expect(mockPush).toHaveBeenCalledWith('/ask/a-2');
});

test('dismissing a notification opens nothing', () => {
  mockResponse = { ...tap('n-3', { ask_id: 'a-3' }), actionIdentifier: 'dismiss' };
  renderHook(() => useNotificationTaps(true));
  expect(mockPush).not.toHaveBeenCalled();
});

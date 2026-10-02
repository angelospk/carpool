import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import { DrivingPrompt } from '../DrivingPrompt';
import type { TripSummary } from '../../api/endpoints';

const mockPush = jest.fn();
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush }),
  useFocusEffect: (cb: () => void) => {
    const { useEffect } = jest.requireActual('react');
    useEffect(cb, [cb]);
  },
}));
jest.mock('../../state/session', () => ({
  useSession: (pick: (s: unknown) => unknown) => pick({ me: { id: 'me' } }),
}));

let mockOnDriving: ((trip: string) => void) | null = null;
const mockRemove = jest.fn();
const mockOn = jest.fn((_account: string | undefined) => Promise.resolve(true));
const mockNow = jest.fn((_trips: unknown, _at: number) => Promise.resolve<string | null>(null));
const mockSync = jest.fn((_trips: unknown, _account: string | undefined) => Promise.resolve());
jest.mock('../../drive/detection', () => ({
  detectionOn: (account: string | undefined) => mockOn(account),
  drivingNow: (trips: unknown, at: number) => mockNow(trips, at),
  syncDetectionWindows: (trips: unknown, account: string | undefined) => mockSync(trips, account),
  watchDriving: async (_trips: unknown, _at: number, cb: (trip: string) => void) => {
    mockOnDriving = cb;
    return { remove: mockRemove };
  },
}));

const trips = [{ id: 't1', role: 'driver' } as TripSummary];

beforeEach(() => {
  jest.clearAllMocks();
  mockOnDriving = null;
});

test('a driver already in the car is asked when they leave', async () => {
  mockNow.mockResolvedValueOnce('t1');
  render(<DrivingPrompt trips={trips} />);
  fireEvent.press(await screen.findByTestId('driving-prompt-go'));
  expect(mockPush).toHaveBeenCalledWith({ pathname: '/trip/[id]', params: { id: 't1' } });
  expect(mockSync).toHaveBeenCalledWith(trips, 'me');
});

// Standing by the car when the screen looked, moving off a minute later.
test('moving off while the screen is open brings the prompt up', async () => {
  const { unmount } = render(<DrivingPrompt trips={trips} />);
  await waitFor(() => expect(mockOnDriving).not.toBeNull());
  expect(screen.queryByTestId('driving-prompt')).toBeNull();
  act(() => mockOnDriving?.('t1'));
  expect(await screen.findByTestId('driving-prompt')).toBeTruthy();
  unmount();
  expect(mockRemove).toHaveBeenCalled();
});

test('says nothing when this account has not turned it on, though the windows are kept in step', async () => {
  mockOn.mockResolvedValueOnce(false);
  render(<DrivingPrompt trips={trips} />);
  await waitFor(() => expect(mockSync).toHaveBeenCalledWith(trips, 'me'));
  expect(mockNow).not.toHaveBeenCalled();
  expect(screen.queryByTestId('driving-prompt')).toBeNull();
});

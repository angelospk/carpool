import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import { DrivingPrompt } from '../DrivingPrompt';
import type { TripSummary } from '../../api/endpoints';

const mockPush = jest.fn();
jest.mock('expo-router', () => ({ useRouter: () => ({ push: mockPush }) }));

const mockOn = jest.fn(() => Promise.resolve(true));
const mockNow = jest.fn((_trips: unknown, _at: number) => Promise.resolve<string | null>('t1'));
const mockSync = jest.fn((_trips: unknown) => Promise.resolve());
jest.mock('../../drive/detection', () => ({
  detectionOn: () => mockOn(),
  drivingNow: (trips: unknown, at: number) => mockNow(trips, at),
  syncDetectionWindows: (trips: unknown) => mockSync(trips),
}));

const trips = [{ id: 't1', role: 'driver' } as TripSummary];

beforeEach(() => jest.clearAllMocks());

test('a driver who seems to be in the car is asked when they leave', async () => {
  render(<DrivingPrompt trips={trips} />);
  fireEvent.press(await screen.findByTestId('driving-prompt-go'));
  expect(mockPush).toHaveBeenCalledWith({ pathname: '/trip/[id]', params: { id: 't1' } });
  expect(mockSync).toHaveBeenCalledWith(trips);
});

test('says nothing when detection is off, though the windows are still kept', async () => {
  mockOn.mockResolvedValueOnce(false);
  render(<DrivingPrompt trips={trips} />);
  await waitFor(() => expect(mockSync).toHaveBeenCalled());
  expect(mockNow).not.toHaveBeenCalled();
  expect(screen.queryByTestId('driving-prompt')).toBeNull();
});

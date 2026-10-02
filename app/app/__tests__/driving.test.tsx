import { render, screen, waitFor } from '@testing-library/react-native';

import DrivingScreen from '../driving';
import { api } from '../../src/api/endpoints';

const mockReplace = jest.fn();
jest.mock('expo-router', () => ({ useRouter: () => ({ replace: mockReplace }) }));
jest.mock('../../src/api/endpoints', () => ({ api: { upcomingTrips: jest.fn() } }));

const HOUR = 3_600_000;

beforeEach(() => jest.clearAllMocks());

// Where a Shortcuts automation lands: the driver's departure near now.
test('opens the departure near now', async () => {
  (api.upcomingTrips as jest.Mock).mockResolvedValue({
    trips: [
      {
        id: 'later',
        role: 'driver',
        status: 'scheduled',
        depart_earliest: new Date(Date.now() + 5 * HOUR).toISOString(),
        depart_latest: new Date(Date.now() + 6 * HOUR).toISOString(),
      },
      {
        id: 'now',
        role: 'driver',
        status: 'scheduled',
        depart_earliest: new Date(Date.now() + 0.2 * HOUR).toISOString(),
        depart_latest: new Date(Date.now() + 0.5 * HOUR).toISOString(),
      },
    ],
  });
  render(<DrivingScreen />);
  await waitFor(() =>
    expect(mockReplace).toHaveBeenCalledWith({ pathname: '/trip/[id]', params: { id: 'now' } }),
  );
});

test('says there is none when there is none', async () => {
  (api.upcomingTrips as jest.Mock).mockResolvedValue({ trips: [] });
  render(<DrivingScreen />);
  expect(await screen.findByTestId('driving-none')).toBeTruthy();
  expect(mockReplace).not.toHaveBeenCalled();
});

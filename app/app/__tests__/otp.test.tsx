import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';

import OtpScreen from '../(auth)/otp';
import { api } from '../../src/api/endpoints';

const mockPush = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush, replace: jest.fn() }),
  useLocalSearchParams: () => ({ phone: '+306900000103', devCode: '123456' }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { regions: jest.fn() },
}));

const region = { id: 'r1', name: 'Έβρος', fuel_price_eur: 1.8 };

describe('the code screen', () => {
  afterEach(() => jest.clearAllMocks());

  it('does not let you continue before the regions have arrived', async () => {
    // The next screen picks its region from this list and has no way to fetch
    // its own. Continuing early carries an empty list forward, which leaves its
    // submit button disabled with nothing on screen to explain why, and no
    // later response can repair it because this screen is gone.
    let release!: (v: { regions: typeof region[] }) => void;
    (api.regions as jest.Mock).mockReturnValue(new Promise((r) => (release = r)));

    render(<OtpScreen />);
    fireEvent.press(screen.getByTestId('otp-submit'));
    expect(mockPush).not.toHaveBeenCalled();

    release({ regions: [region] });
    await waitFor(() => expect(screen.getByTestId('otp-submit')).toBeTruthy());
    fireEvent.press(screen.getByTestId('otp-submit'));
    await waitFor(() => expect(mockPush).toHaveBeenCalled());
  });

  it('says so when the regions cannot be fetched, instead of going on regardless', async () => {
    (api.regions as jest.Mock).mockRejectedValue(new Error('offline'));

    render(<OtpScreen />);
    await waitFor(() => expect(screen.getByText(/Δεν/)).toBeTruthy());
    fireEvent.press(screen.getByTestId('otp-submit'));
    expect(mockPush).not.toHaveBeenCalled();
  });

  it('carries the regions it fetched to the next screen', async () => {
    (api.regions as jest.Mock).mockResolvedValue({ regions: [region] });

    render(<OtpScreen />);
    await waitFor(() => expect(api.regions).toHaveBeenCalled());
    fireEvent.press(screen.getByTestId('otp-submit'));

    await waitFor(() => expect(mockPush).toHaveBeenCalled());
    const params = mockPush.mock.calls[0][0].params;
    expect(JSON.parse(params.regions)).toEqual([region]);
    expect(params.code).toBe('123456');
  });
});

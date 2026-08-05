import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';

import OtpScreen from '../(auth)/otp';
import { api } from '../../src/api/endpoints';
import { useSession } from '../../src/state/session';

const mockPush = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush, replace: jest.fn() }),
  useLocalSearchParams: () => ({ phone: '+306900000103', devCode: '123456' }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { verifyOtp: jest.fn() },
}));

jest.mock('../../src/state/session', () => ({
  useSession: jest.fn(),
}));

const mockSignIn = jest.fn();

beforeEach(() => {
  jest.clearAllMocks();
  (useSession as unknown as jest.Mock).mockImplementation((select: (s: unknown) => unknown) =>
    select({ signIn: mockSignIn }),
  );
});

describe('the code screen', () => {
  // The whole point of the change. Somebody who joined in March opens the app
  // in April, types the code, and is in — no name, no town, no list of
  // seventy-five prefectures.
  it('signs an existing account straight in', async () => {
    (api.verifyOtp as jest.Mock).mockResolvedValue({
      access_token: 'a',
      refresh_token: 'r',
    });

    render(<OtpScreen />);
    fireEvent.press(screen.getByTestId('otp-submit'));

    await waitFor(() =>
      expect(mockSignIn).toHaveBeenCalledWith({ accessToken: 'a', refreshToken: 'r' }),
    );
    expect(mockPush).not.toHaveBeenCalled();
  });

  // A new number gets a ticket instead of a session, and the ticket is what
  // travels — never the code, which has already been spent proving the number.
  it('carries the ticket to the profile screen for a new number', async () => {
    (api.verifyOtp as jest.Mock).mockResolvedValue({ signup_token: 'tkt' });

    render(<OtpScreen />);
    fireEvent.press(screen.getByTestId('otp-submit'));

    await waitFor(() => expect(mockPush).toHaveBeenCalled());
    const { pathname, params } = mockPush.mock.calls[0][0];
    expect(pathname).toContain('profile-setup');
    expect(params.signupToken).toBe('tkt');
    expect(params.code).toBeUndefined();
    expect(mockSignIn).not.toHaveBeenCalled();
  });

  it('says the code was wrong rather than going on', async () => {
    const { ApiError } = jest.requireActual('../../src/api/client');
    (api.verifyOtp as jest.Mock).mockRejectedValue(new ApiError(401, 'invalid_code', 'no'));

    render(<OtpScreen />);
    fireEvent.press(screen.getByTestId('otp-submit'));

    expect(await screen.findByTestId('otp-error')).toBeTruthy();
    expect(mockPush).not.toHaveBeenCalled();
    expect(mockSignIn).not.toHaveBeenCalled();
  });
});

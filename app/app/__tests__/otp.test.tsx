import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';

import OtpScreen from '../(auth)/otp';
import { api } from '../../src/api/endpoints';
import { useSession } from '../../src/state/session';

const mockPush = jest.fn();
const mockReplace = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush, replace: mockReplace }),
  useLocalSearchParams: () => ({ phone: '+306900000103', devCode: '123456' }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { verifyOtp: jest.fn(), requestOtp: jest.fn() },
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

// A code that never arrives, or a number typed wrong, left nothing to press:
// the screen had one button and it needed the code.
describe('getting out of the code screen', () => {
  it('says which number the code went to', () => {
    render(<OtpScreen />);
    expect(screen.getByText(/\+306900000103/)).toBeTruthy();
  });

  it('goes back to change the number', () => {
    render(<OtpScreen />);
    fireEvent.press(screen.getByTestId('otp-change-number'));
    expect(mockReplace).toHaveBeenCalledWith('/(auth)/phone');
  });

  it('asks for a new code and says it did', async () => {
    (api.requestOtp as jest.Mock).mockResolvedValue({ dev_code: '654321' });
    render(<OtpScreen />);

    fireEvent.press(screen.getByTestId('otp-resend'));

    await waitFor(() => expect(api.requestOtp).toHaveBeenCalledWith('+306900000103'));
    expect(await screen.findByText('Σου στείλαμε νέο κωδικό.')).toBeTruthy();
  });

  it('a refused resend says so rather than claiming a code went out', async () => {
    (api.requestOtp as jest.Mock).mockRejectedValue(new Error('429'));
    render(<OtpScreen />);

    fireEvent.press(screen.getByTestId('otp-resend'));

    await waitFor(() => expect(api.requestOtp).toHaveBeenCalled());
    expect(screen.queryByText('Σου στείλαμε νέο κωδικό.')).toBeNull();
  });
});

// A resend that lands after the code was checked would issue a new code over
// the one just used. One request at a time.
describe('one request at a time', () => {
  it('cannot check the code while a new one is being asked for', async () => {
    (api.requestOtp as jest.Mock).mockReturnValue(new Promise(() => {}));
    render(<OtpScreen />);

    fireEvent.press(screen.getByTestId('otp-resend'));
    await waitFor(() => expect(api.requestOtp).toHaveBeenCalled());
    fireEvent.press(screen.getByTestId('otp-submit'));
    expect(api.verifyOtp).not.toHaveBeenCalled();
  });

  it('cannot resend or change number while the code is being checked', async () => {
    (api.verifyOtp as jest.Mock).mockReturnValue(new Promise(() => {}));
    render(<OtpScreen />);

    fireEvent.press(screen.getByTestId('otp-submit'));
    await waitFor(() => expect(api.verifyOtp).toHaveBeenCalled());
    fireEvent.press(screen.getByTestId('otp-resend'));
    fireEvent.press(screen.getByTestId('otp-change-number'));
    expect(api.requestOtp).not.toHaveBeenCalled();
    expect(mockReplace).not.toHaveBeenCalled();
  });
});

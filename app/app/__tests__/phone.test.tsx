import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';

import PhoneScreen from '../(auth)/phone';
import { api } from '../../src/api/endpoints';
import { NetworkError, ApiError } from '../../src/api/client';

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { requestOtp: jest.fn() },
}));

jest.mock('../../src/api/client', () => {
  const actual = jest.requireActual('../../src/api/client');
  return { ApiError: actual.ApiError, NetworkError: actual.NetworkError, currentBaseUrl: () => 'http://192.168.0.154:8080' };
});

const submit = async (phone: string) => {
  render(<PhoneScreen />);
  fireEvent.changeText(screen.getByTestId('phone-input'), phone);
  fireEvent.press(screen.getByTestId('phone-submit'));
};

describe('the phone screen when the request fails', () => {
  afterEach(() => jest.clearAllMocks());

  it('names the server it could not reach, rather than saying nothing', async () => {
    // The failure that cost hours: a web build opened over a different network
    // than it was exported for cannot route to the baked-in address. There is
    // no status code and no server-side log, so if the screen does not say
    // which address it tried, nothing anywhere does.
    (api.requestOtp as jest.Mock).mockRejectedValue(new NetworkError(new Error('failed to fetch')));
    await submit('+306900000103');
    await waitFor(() => expect(screen.getByText(/192\.168\.0\.154:8080/)).toBeTruthy());
  });

  it('still separates a rate limit from everything else', async () => {
    (api.requestOtp as jest.Mock).mockRejectedValue(
      new ApiError(429, 'rate_limited', 'too many codes requested for this number'),
    );
    await submit('+306900000103');
    await waitFor(() => expect(screen.getByText(/Πολλές προσπάθειες/)).toBeTruthy());
  });

  it('falls back to the generic message when the server rejects for its own reasons', async () => {
    (api.requestOtp as jest.Mock).mockRejectedValue(new ApiError(400, 'invalid_request', 'bad phone'));
    await submit('+306900000103');
    await waitFor(() => expect(screen.getByText(/Κάτι πήγε στραβά/)).toBeTruthy());
  });
});

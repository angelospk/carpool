import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';

import ProfileSetupScreen from '../(auth)/profile-setup';
import { api } from '../../src/api/endpoints';
import { useSession } from '../../src/state/session';

jest.mock('expo-router', () => ({
  useLocalSearchParams: () => ({ signupToken: 'tkt' }),
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { completeSignup: jest.fn(), places: jest.fn() },
}));

jest.mock('../../src/state/session', () => ({
  useSession: jest.fn(),
}));

const mockSignIn = jest.fn();

const feres = {
  id: 'node/139260653',
  name: 'Φέρες',
  area: 'Περιφερειακή Ενότητα Έβρου',
  lon: 26.174,
  lat: 40.894,
  kind: 'town',
};

beforeEach(() => {
  jest.clearAllMocks();
  (useSession as unknown as jest.Mock).mockImplementation((select: (s: unknown) => unknown) =>
    select({ signIn: mockSignIn }),
  );
  (api.places as jest.Mock).mockResolvedValue({ places: [feres] });
  (api.completeSignup as jest.Mock).mockResolvedValue({
    access_token: 'a',
    refresh_token: 'r',
  });
});

// Choosing a town, not a prefecture. Nobody knows they live in "Περιφερειακή
// Ενότητα Δυτικού Τομέα Αθηνών"; everybody knows the name of their town, and
// the server can look the rest up.
async function pickHomeTown() {
  fireEvent.changeText(screen.getByTestId('home-place-input'), 'Φέρ');
  const option = await screen.findByTestId(`home-place-${feres.id}`);
  fireEvent.press(option);
}

describe('the profile screen', () => {
  it('sends the name, the town and the ticket, then signs in', async () => {
    render(<ProfileSetupScreen />);

    fireEvent.changeText(screen.getByTestId('name-input'), 'Χαράλαμπος');
    await pickHomeTown();
    fireEvent.press(screen.getByTestId('profile-submit'));

    await waitFor(() => expect(api.completeSignup).toHaveBeenCalled());
    expect(api.completeSignup).toHaveBeenCalledWith(
      expect.objectContaining({
        signup_token: 'tkt',
        name: 'Χαράλαμπος',
        home_place_id: feres.id,
      }),
    );
    await waitFor(() =>
      expect(mockSignIn).toHaveBeenCalledWith({ accessToken: 'a', refreshToken: 'r' }),
    );
  });

  it('will not submit without both a name and a town', async () => {
    render(<ProfileSetupScreen />);

    const submit = () => screen.getByTestId('profile-submit');
    expect(submit().props.accessibilityState?.disabled).toBe(true);

    fireEvent.changeText(screen.getByTestId('name-input'), 'Χαράλαμπος');
    expect(submit().props.accessibilityState?.disabled).toBe(true);

    await pickHomeTown();
    expect(submit().props.accessibilityState?.disabled).toBe(false);
  });

  // The ticket lasts ten minutes. Somebody who left the phone on the table
  // needs to be told to start again, not shown "something went wrong".
  it('says the sign-in has expired when the ticket is refused', async () => {
    const { ApiError } = jest.requireActual('../../src/api/client');
    (api.completeSignup as jest.Mock).mockRejectedValue(
      new ApiError(401, 'invalid_token', 'expired'),
    );

    render(<ProfileSetupScreen />);
    fireEvent.changeText(screen.getByTestId('name-input'), 'Χαράλαμπος');
    await pickHomeTown();
    fireEvent.press(screen.getByTestId('profile-submit'));

    const error = await screen.findByTestId('profile-error');
    expect(error.props.children).toMatch(/ξανά|νέο/i);
    expect(mockSignIn).not.toHaveBeenCalled();
  });
});

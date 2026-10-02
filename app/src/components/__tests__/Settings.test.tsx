import {
  act,
  render as rtlRender,
  screen,
  fireEvent,
  waitFor,
} from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactElement } from 'react';

import i18n from '../../i18n';
import { Settings } from '../Settings';
import { api } from '../../api/endpoints';
import { ApiError } from '../../api/client';

jest.mock('../../api/endpoints', () => ({ api: { updateMe: jest.fn() } }));

const mockEnable = jest.fn((_account: string, _notice: unknown) => Promise.resolve(true));
const mockDisable = jest.fn(() => Promise.resolve());
jest.mock('../../drive/detection', () => ({
  detectionOn: () => Promise.resolve(false),
  enableDetection: (account: string, notice: unknown) => mockEnable(account, notice),
  disableDetection: () => mockDisable(),
}));

let mockMe: Record<string, unknown> = {};
const mockSetMe = jest.fn();
jest.mock('../../state/session', () => {
  const useSession = (pick: (s: unknown) => unknown) => pick({ me: mockMe, setMe: mockSetMe });
  useSession.getState = () => ({ me: mockMe });
  return { useSession };
});

function render(node: ReactElement) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return rtlRender(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
}

beforeEach(() => {
  jest.clearAllMocks();
  mockMe = { id: 'me', gender: null, women_only: false };
  (api.updateMe as jest.Mock).mockImplementation(async (patch) => ({ ...mockMe, ...patch }));
});

afterEach(() => void i18n.changeLanguage('el'));

// The setting was a row that said "off" and could not be changed, while the
// server had honoured it for weeks.
test('a woman can turn on women drivers only', async () => {
  mockMe = { id: 'me', gender: 'female', women_only: false };
  render(<Settings />);

  fireEvent(screen.getByTestId('women-only'), 'valueChange', true);

  await waitFor(() => expect(api.updateMe).toHaveBeenCalledWith({ women_only: true }));
  expect(mockSetMe).toHaveBeenCalledWith(expect.objectContaining({ women_only: true }));
});

// The server refuses it for anybody else, so the switch is not offered as if
// it would work; it says what to set first.
test('without a gender the switch is shut and says why', () => {
  render(<Settings />);

  expect(screen.getByTestId('women-only').props.disabled).toBe(true);
  expect(screen.getByTestId('women-only-hint')).toBeTruthy();
});

test('choosing a gender saves it', async () => {
  render(<Settings />);

  fireEvent.press(screen.getByTestId('gender-female'));

  await waitFor(() => expect(api.updateMe).toHaveBeenCalledWith({ gender: 'female' }));
});

test('a refusal is said in words and the switch goes back', async () => {
  mockMe = { id: 'me', gender: 'female', women_only: false };
  (api.updateMe as jest.Mock).mockRejectedValue(
    new ApiError(400, 'women_only_needs_female', 'no'),
  );
  render(<Settings />);

  fireEvent(screen.getByTestId('women-only'), 'valueChange', true);

  expect(await screen.findByTestId('settings-error')).toHaveTextContent(/φύλο/);
  expect(screen.getByTestId('women-only').props.value).toBe(false);
});

// The language row said "Ελληνικά" and did nothing when pressed.
test('the language can be switched', async () => {
  render(<Settings />);

  fireEvent.press(screen.getByTestId('language-en'));

  await waitFor(() => expect(i18n.language).toBe('en'));
});

// The matcher skips a paused driver; nothing could pause one.
test('a driver can pause asks, and a passenger is not offered it', async () => {
  mockMe = { id: 'me', gender: null, women_only: false, asks_paused: false };
  const { unmount } = render(<Settings />);

  fireEvent(screen.getByTestId('asks-paused'), 'valueChange', true);
  await waitFor(() => expect(api.updateMe).toHaveBeenCalledWith({ asks_paused: true }));
  unmount();

  mockMe = { id: 'me', gender: null, women_only: false };
  render(<Settings />);
  expect(screen.queryByTestId('asks-paused')).toBeNull();
});

// A change still in flight when somebody signs out, or in as somebody else,
// must not land on whoever is there when it answers.
test.each([
  ['somebody else', { id: 'someone-else', gender: null, women_only: false }],
  ['nobody', null],
])('an answer that arrives for %s is dropped', async (_who, after) => {
  let answer: (v: unknown) => void = () => {};
  (api.updateMe as jest.Mock).mockImplementation(
    () => new Promise((resolve) => (answer = resolve)),
  );
  mockMe = { id: 'me', gender: 'female', women_only: false };
  render(<Settings />);

  fireEvent(screen.getByTestId('women-only'), 'valueChange', true);
  // The request has gone out before anything changes underneath it.
  await waitFor(() => expect(api.updateMe).toHaveBeenCalled());
  mockMe = after as typeof mockMe;
  await act(async () => {
    answer({ id: 'me', gender: 'female', women_only: true });
  });

  expect(mockSetMe).not.toHaveBeenCalled();
});

// The same answer for the account that asked is applied, which is what makes
// the test above mean something.
test('an answer for the account that asked is applied', async () => {
  let answer: (v: unknown) => void = () => {};
  (api.updateMe as jest.Mock).mockImplementation(
    () => new Promise((resolve) => (answer = resolve)),
  );
  mockMe = { id: 'me', gender: 'female', women_only: false };
  render(<Settings />);

  fireEvent(screen.getByTestId('women-only'), 'valueChange', true);
  await waitFor(() => expect(api.updateMe).toHaveBeenCalled());
  await act(async () => {
    answer({ id: 'me', gender: 'female', women_only: true });
  });

  expect(mockSetMe).toHaveBeenCalledWith(expect.objectContaining({ women_only: true }));
});

// Changing gender away from female turns women drivers only off on the
// server. The switch has to follow, not keep showing "on".
test('women drivers only follows the server when gender changes', async () => {
  mockMe = { id: 'me', gender: 'female', women_only: true };
  (api.updateMe as jest.Mock).mockResolvedValue({ id: 'me', gender: 'male', women_only: false });
  render(<Settings />);

  fireEvent.press(screen.getByTestId('gender-male'));

  await waitFor(() => expect(mockSetMe).toHaveBeenCalled());
  expect(mockSetMe).toHaveBeenCalledWith(
    expect.objectContaining({ gender: 'male', women_only: false }),
  );
});

// This card's fields come from the answer; everything else stays as it is now.
// A photo uploaded while the change was in flight is newer than the answer.
test("the answer does not undo a photo changed while it was in flight", async () => {
  let answer: (v: unknown) => void = () => {};
  (api.updateMe as jest.Mock).mockImplementation(
    () => new Promise((resolve) => (answer = resolve)),
  );
  mockMe = { id: 'me', gender: null, women_only: false, photo_url: 'old.jpg' };
  render(<Settings />);

  fireEvent.press(screen.getByTestId('gender-female'));
  await waitFor(() => expect(api.updateMe).toHaveBeenCalled());
  mockMe = { ...mockMe, photo_url: 'new.jpg' };
  await act(async () => {
    answer({ id: 'me', gender: 'female', women_only: false, photo_url: 'old.jpg' });
  });

  expect(mockSetMe).toHaveBeenCalledWith(
    expect.objectContaining({ gender: 'female', photo_url: 'new.jpg' }),
  );
});

test('the other controls are shut while one change is saving', async () => {
  (api.updateMe as jest.Mock).mockImplementation(() => new Promise(() => {}));
  mockMe = { id: 'me', gender: 'female', women_only: false };
  render(<Settings />);

  fireEvent.press(screen.getByTestId('gender-male'));

  await waitFor(() => expect(screen.getByTestId('gender-other')).toBeDisabled());
  expect(screen.getByTestId('women-only').props.disabled).toBe(true);
});

// Noticing when a driver sets off is theirs to turn on, and the switch shows
// what the phone agreed to.
test('a driver turns on noticing when they drive', async () => {
  mockMe = { id: 'me', gender: null, women_only: false, asks_paused: false };
  render(<Settings />);
  fireEvent(screen.getByTestId('detect-driving'), 'valueChange', true);
  await waitFor(() => expect(mockEnable).toHaveBeenCalledWith('me', expect.anything()));
  await waitFor(() => expect(screen.getByTestId('detect-driving').props.value).toBe(true));
});

test('a refused permission leaves it off and says why', async () => {
  mockMe = { id: 'me', gender: null, women_only: false, asks_paused: false };
  mockEnable.mockResolvedValueOnce(false);
  render(<Settings />);
  fireEvent(screen.getByTestId('detect-driving'), 'valueChange', true);
  expect(await screen.findByTestId('settings-error')).toHaveTextContent(/δραστηριότητα/);
  expect(screen.getByTestId('detect-driving').props.value).toBe(false);
});

test('somebody with no car is not offered it', () => {
  render(<Settings />);
  expect(screen.queryByTestId('detect-driving')).toBeNull();
});

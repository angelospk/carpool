import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import RequestScreen from '../request/[id]';
import { ApiError } from '../../src/api/client';
import { api } from '../../src/api/endpoints';
import { shareText } from '../../src/components/shareText';
import { usePushReach } from '../../src/push/reach';
import { Alert, Linking } from 'react-native';

const mockReplace = jest.fn();
const mockPush = jest.fn();

jest.mock('expo-router', () => ({
  useLocalSearchParams: () => ({ id: 'req-1' }),
  useRouter: () => ({ replace: mockReplace, push: mockPush }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { cancelRequest: jest.fn(), myRequests: jest.fn(), requestAlternatives: jest.fn() },
}));

jest.mock('../../src/components/shareText', () => ({
  shareText: jest.fn(async () => 'shared'),
}));

jest.mock('../../src/state/session', () => ({
  useSession: (select: (s: unknown) => unknown) => select({ status: 'signedIn' }),
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

// Cancelling asks first. Every test below that presses it answers yes.
let alert: jest.SpyInstance;
beforeEach(() => {
  jest.clearAllMocks();
  alert = jest
    .spyOn(Alert, 'alert')
    .mockImplementation((_title, _message, buttons) => buttons?.[buttons.length - 1]?.onPress?.());
  (api.myRequests as jest.Mock).mockResolvedValue({ requests: [] });
});

// The request the cancel tests are about. Cancelling is only offered once the
// screen knows what it is cancelling, so these three have to start from a
// request that is actually in the open list.
const asking = {
  id: 'req-1',
  origin_label: 'Αλεξανδρούπολη',
  destination_label: 'Κομοτηνή',
  window_start: '2026-07-29T04:30:00Z',
  window_end: '2026-07-29T06:30:00Z',
  seats: 1,
  status: 'asking',
  drivers_asked: 2,
  drivers_declined: 1,
  asking_now: true,
  drivers_on_corridor: 1,
  created_at: '2026-07-29T04:00:00Z',
};

// The card on the home screen says how far the search has got. Opening it used
// to show a pulsing dot over a raw uuid, which is less than the card said.
test('the screen says the same thing the card that led here said', async () => {
  (api.myRequests as jest.Mock).mockResolvedValue({
    requests: [
      {
        id: 'req-1',
        origin_label: 'Αλεξανδρούπολη',
        destination_label: 'Κομοτηνή',
        window_start: '2026-07-29T04:30:00Z',
        window_end: '2026-07-29T06:30:00Z',
        seats: 1,
        status: 'asking',
        drivers_asked: 2,
        drivers_declined: 1,
        asking_now: true,
        created_at: '2026-07-29T04:00:00Z',
      },
    ],
  });

  render(wrap(<RequestScreen />));

  await waitFor(() => expect(screen.getByTestId('request-route')).toBeTruthy());
  expect(screen.getByTestId('request-progress').props.children).toContain('τώρα');
  expect(screen.queryByText('req-1')).toBeNull();
});

// A request that has been matched, cancelled or has simply not arrived yet is
// not in the list, and none of those is worth guessing at out loud.
test('a request that is not in the list falls back to the general wording', async () => {
  render(wrap(<RequestScreen />));

  await waitFor(() => expect(api.myRequests).toHaveBeenCalled());
  expect(screen.queryByTestId('request-progress')).toBeNull();
});

// One tap used to cancel the request, with a driver possibly reading it
// (QA 2026-10-16, U7).
test('cancelling asks first, and saying no keeps the request', async () => {
  alert.mockImplementation(() => {});
  (api.myRequests as jest.Mock).mockResolvedValue({ requests: [asking] });

  render(wrap(<RequestScreen />));
  fireEvent.press(await screen.findByTestId('cancel-request'));

  await waitFor(() => expect(alert).toHaveBeenCalled());
  expect(api.cancelRequest).not.toHaveBeenCalled();
});

test('cancelling a request that is still asking takes them home', async () => {
  (api.cancelRequest as jest.Mock).mockResolvedValue(undefined);

  (api.myRequests as jest.Mock).mockResolvedValue({ requests: [asking] });

  render(wrap(<RequestScreen />));
  fireEvent.press(await screen.findByTestId('cancel-request'));

  await waitFor(() => expect(mockReplace).toHaveBeenCalled());
});

// This screen is where somebody sits while drivers are asked, so a driver
// accepting *while it is open* is the ordinary case rather than a rare race.
// Leaving is the one thing that must not happen: it would tell them they are
// not going while a seat is held in their name and a driver is on the way.
test('a driver taking the ride keeps them on the screen and says so', async () => {
  (api.cancelRequest as jest.Mock).mockRejectedValue(
    new ApiError(409, 'already_matched', 'a driver has taken this ride'),
  );

  (api.myRequests as jest.Mock).mockResolvedValue({ requests: [asking] });

  render(wrap(<RequestScreen />));
  fireEvent.press(await screen.findByTestId('cancel-request'));

  await waitFor(() => expect(screen.getByTestId('request-outcome')).toBeTruthy());
  expect(mockReplace).not.toHaveBeenCalled();
  // "Looking for a seat" is no longer true, so it is no longer offered.
  expect(screen.queryByTestId('request-share')).toBeNull();
});

test('a failure to cancel is not read as a cancellation', async () => {
  (api.cancelRequest as jest.Mock).mockRejectedValue(new ApiError(500, 'internal', 'boom'));

  (api.myRequests as jest.Mock).mockResolvedValue({ requests: [asking] });

  render(wrap(<RequestScreen />));
  fireEvent.press(await screen.findByTestId('cancel-request'));

  await waitFor(() => expect(screen.getByTestId('request-outcome')).toBeTruthy());
  expect(mockReplace).not.toHaveBeenCalled();
});

// A passenger whose driver took the seat back is told so, told the search
// restarted without them, and shown the cars still on the road in the order
// they will be asked — with no driver named, because none of them has agreed
// to anything.
test('a stranded request says the driver cancelled and lists the cars still on the road', async () => {
  (api.myRequests as jest.Mock).mockResolvedValue({ requests: [{ ...asking, stranded: true }] });
  (api.requestAlternatives as jest.Mock).mockResolvedValue({
    alternatives: [
      {
        depart_earliest: '2026-07-29T05:00:00Z',
        depart_latest: '2026-07-29T05:30:00Z',
        origin_label: 'Αλεξανδρούπολη',
        destination_label: 'Κομοτηνή',
        seats_free: 2,
      },
    ],
  });

  render(wrap(<RequestScreen />));

  await waitFor(() => expect(screen.getByTestId('request-stranded')).toBeTruthy());
  expect(String(screen.getByTestId('request-progress').props.children)).toContain('Ο οδηγός ακύρωσε');
  await waitFor(() => expect(screen.getByTestId('alternative-0')).toBeTruthy());
  const line = [screen.getByTestId('alternative-0').props.children].flat().join('');
  expect(line).toContain('08:00');
  expect(line).toContain('2 θέσεις');
  expect(api.requestAlternatives).toHaveBeenCalledWith('req-1');
  expect(screen.getByTestId('request-bus')).toBeTruthy();
});

// An ordinary request does not ask for alternatives at all: the dispatcher is
// choosing, and a list would read as a choice the passenger has to make.
test('an ordinary request does not look for alternatives', async () => {
  (api.myRequests as jest.Mock).mockResolvedValue({ requests: [asking] });
  render(wrap(<RequestScreen />));
  await waitFor(() => expect(screen.getByTestId('request-progress')).toBeTruthy());
  expect(screen.queryByTestId('request-stranded')).toBeNull();
  expect(api.requestAlternatives).not.toHaveBeenCalled();
});

// QA found this: "we are asking the next one" and "waiting for an answer"
// directly above "there is no other car on your road".
test('a stranded request with nobody left says so without claiming a search', async () => {
  (api.myRequests as jest.Mock).mockResolvedValue({
    requests: [{ ...asking, stranded: true, asking_now: false }],
  });
  (api.requestAlternatives as jest.Mock).mockResolvedValue({ alternatives: [] });
  render(wrap(<RequestScreen />));
  await waitFor(() => expect(screen.getByTestId('request-no-alternatives')).toBeTruthy());
  const title = String(screen.getByTestId('request-stranded-title').props.children);
  expect(title).not.toContain('ρωτάμε ήδη');
  expect(String(screen.getByTestId('request-progress').props.children)).not.toContain('Περιμένουμε');
});

test('alternatives that fail to load say so and can be retried', async () => {
  (api.myRequests as jest.Mock).mockResolvedValue({ requests: [{ ...asking, stranded: true }] });
  (api.requestAlternatives as jest.Mock).mockRejectedValue(new Error('offline'));
  render(wrap(<RequestScreen />));
  await waitFor(() => expect(screen.getByTestId('request-alternatives-failed')).toBeTruthy());
  expect(screen.getByTestId('request-alternatives-retry')).toBeTruthy();
});

// The driver's side has always had a sentence to paste into the village group.
// The passenger, who is the larger side, had nothing to send.
test('a request can be sent to a group as a sentence', async () => {
  (api.myRequests as jest.Mock).mockResolvedValue({ requests: [asking] });

  render(wrap(<RequestScreen />));
  fireEvent.press(await screen.findByTestId('request-share'));

  await waitFor(() => expect(shareText).toHaveBeenCalled());
  const [message] = (shareText as jest.Mock).mock.calls[0];
  expect(message).toMatch(/^Ψάχνω θέση · Αλεξανδρούπολη → Κομοτηνή · /);
});

test('there is nothing to share for a request that is no longer open', async () => {
  render(wrap(<RequestScreen />));

  await screen.findByTestId('request-closed');
  expect(screen.queryByTestId('request-share')).toBeNull();
});

describe('what the screen promises about being told', () => {
  afterEach(() => usePushReach.setState({ on: false }));

  // Push is off on every build until the Expo and Firebase projects exist, and
  // always off on the web. «Θα σου έρθει ειδοποίηση» then sends somebody away
  // from the only place the answer will appear.
  it('does not promise a notification this device cannot get', async () => {
    (api.myRequests as jest.Mock).mockResolvedValue({ requests: [asking] });
    render(wrap(<RequestScreen />));

    const leave = await screen.findByTestId('request-leave');
    expect(String(leave.props.children)).not.toContain('ειδοποίηση');
    expect(String(leave.props.children)).toContain('ενημερώνεται');
  });

  it('says it may be closed once notifications reach this device', async () => {
    usePushReach.setState({ on: true });
    (api.myRequests as jest.Mock).mockResolvedValue({ requests: [asking] });
    render(wrap(<RequestScreen />));

    const leave = await screen.findByTestId('request-leave');
    expect(String(leave.props.children)).toContain('Θα σου έρθει ειδοποίηση');
  });
});

// The stack header already says «Το αίτημά σου».
test('names the screen once, in the header', async () => {
  (api.myRequests as jest.Mock).mockResolvedValue({ requests: [asking] });
  render(wrap(<RequestScreen />));
  await screen.findByTestId('request-leave');
  expect(screen.queryByText('Το αίτημά σου')).toBeNull();
});

// The pilot runs along the whole Thracian corridor, and a passenger stranded in
// Κομοτηνή has no use for the Έβρος timetable.
test('the bus fallback is the ΚΤΕΛ of the town the trip starts from', async () => {
  (api.myRequests as jest.Mock).mockResolvedValue({
    requests: [{ ...asking, origin_label: 'Κομοτηνή', destination_label: 'Ξάνθη', stranded: true }],
  });
  (api.requestAlternatives as jest.Mock).mockResolvedValue({ alternatives: [] });
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(true);

  render(wrap(<RequestScreen />));
  const bus = await screen.findByTestId('request-bus');
  expect(screen.getByText('Δρομολόγια ΚΤΕΛ Ροδόπης')).toBeTruthy();
  fireEvent.press(bus);
  expect(open).toHaveBeenCalledWith('https://ktelrodopis.gr/');
});

// QA 2026-10-17 U F6: cancelling one day of a journey asked for on several
// left the others asking. Now it asks which: every day, or only this one.
describe('a journey on more than one day', () => {
  const sibling = { ...asking, id: 'req-2', alternative_of: 'req-1', window_start: '2026-08-05T04:30:00Z' };
  type Button = { text?: string; onPress?: () => void };

  test('cancelling every day says so to the server', async () => {
    (api.cancelRequest as jest.Mock).mockResolvedValue(undefined);
    (api.myRequests as jest.Mock).mockResolvedValue({ requests: [asking, sibling] });
    render(wrap(<RequestScreen />));
    fireEvent.press(await screen.findByTestId('cancel-request'));
    await waitFor(() => expect(api.cancelRequest).toHaveBeenCalledWith('req-1', true));
    const buttons = alert.mock.calls[0][2] as Button[];
    expect(buttons).toHaveLength(3);
  });

  test('cancelling only this day leaves the others', async () => {
    (api.cancelRequest as jest.Mock).mockResolvedValue(undefined);
    (api.myRequests as jest.Mock).mockResolvedValue({ requests: [asking, sibling] });
    alert.mockImplementation((_t, _m, buttons?: Button[]) => buttons?.[1]?.onPress?.());
    render(wrap(<RequestScreen />));
    fireEvent.press(await screen.findByTestId('cancel-request'));
    await waitFor(() => expect(api.cancelRequest).toHaveBeenCalledWith('req-1', false));
  });

  // Codex review: the home card leads to one day only, so each other day must
  // be reachable from here to be cancelled on its own.
  test('the other days open their own request', async () => {
    (api.myRequests as jest.Mock).mockResolvedValue({ requests: [asking, sibling] });
    render(wrap(<RequestScreen />));
    fireEvent.press(await screen.findByTestId('request-day-req-2'));
    expect(mockPush).toHaveBeenCalledWith('/request/req-2');
    expect(screen.queryByTestId('request-day-req-1')).toBeNull();
  });
});

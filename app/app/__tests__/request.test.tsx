import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import RequestScreen from '../request/[id]';
import { ApiError } from '../../src/api/client';
import { api } from '../../src/api/endpoints';

const mockReplace = jest.fn();

jest.mock('expo-router', () => ({
  useLocalSearchParams: () => ({ id: 'req-1' }),
  useRouter: () => ({ replace: mockReplace, push: jest.fn() }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { cancelRequest: jest.fn(), myRequests: jest.fn(), requestAlternatives: jest.fn() },
}));

jest.mock('../../src/state/session', () => ({
  useSession: (select: (s: unknown) => unknown) => select({ status: 'signedIn' }),
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

beforeEach(() => {
  jest.clearAllMocks();
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

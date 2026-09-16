import { act, render, screen, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import RequestScreen from '../request/[id]';
import { api } from '../../src/api/endpoints';
import el from '../../src/i18n/el.json';

jest.mock('expo-router', () => ({
  useLocalSearchParams: () => ({ id: 'req-1' }),
  useRouter: () => ({ replace: jest.fn(), push: jest.fn() }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { cancelRequest: jest.fn(), myRequests: jest.fn() },
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

beforeEach(() => jest.clearAllMocks());

// Four different situations used to render one sentence — and the sentence was
// «δεν βρέθηκε οδηγός», which is a claim about supply. A list that has not
// loaded, a list that failed to load, and a request that has been matched are
// none of them an answer about whether anybody drives that road.

test('a list that has not arrived yet says it is loading, not that nobody drives', async () => {
  let release: (v: unknown) => void = () => {};
  (api.myRequests as jest.Mock).mockReturnValue(new Promise((r) => (release = r)));

  render(wrap(<RequestScreen />));

  expect(screen.getByTestId('request-loading')).toBeTruthy();
  expect(screen.queryByTestId('request-no-drivers')).toBeNull();

  release({ requests: [] });
  await waitFor(() => expect(screen.queryByTestId('request-loading')).toBeNull());
});

test('a list that failed to load says so rather than answering about drivers', async () => {
  (api.myRequests as jest.Mock).mockRejectedValue(new Error('offline'));

  render(wrap(<RequestScreen />));

  expect(await screen.findByTestId('request-failed')).toBeTruthy();
  expect(screen.queryByTestId('request-no-drivers')).toBeNull();
});

// The list polls every thirty seconds, so a passenger sitting on this screen
// meets a transient network failure sooner or later, and the request they are
// reading must not be replaced by an error box and then restored.
//
// It is not, and the reason is subtle enough to be worth pinning down. When a
// refetch rejects over data that is already there, react-query 5 records the
// error in the cache but keeps reporting `success` to the consumer, data
// intact. So `requests.isError` never becomes true here and the screen needs no
// guard of its own — one was tried and removed. This asserts the behaviour
// because it belongs to the library rather than to this file: an upgrade that
// changed it would break this screen silently, and nothing else would notice.
//
// The cache has to be the same across both fetches, which is why this holds the
// QueryClient instead of calling wrap(). An earlier version re-wrapped on
// rerender, handed the screen a brand new cache, and asserted nothing at all
// while passing.
test('a poll that fails leaves the request a passenger is reading alone', async () => {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  (api.myRequests as jest.Mock).mockResolvedValueOnce({
    requests: [
      {
        id: 'req-1',
        origin_label: 'Αλεξανδρούπολη',
        destination_label: 'Ορεστιάδα',
        window_start: '2026-09-17T04:30:00Z',
        window_end: '2026-09-17T06:30:00Z',
        seats: 1,
        status: 'asking',
        drivers_asked: 1,
        drivers_declined: 0,
        asking_now: true,
        drivers_on_corridor: 1,
        created_at: '2026-09-16T06:00:00Z',
      },
    ],
  });

  render(
    <QueryClientProvider client={client}>
      <RequestScreen />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(screen.getByTestId('request-route')).toBeTruthy());

  // The next poll fails, against the answer already in the cache.
  (api.myRequests as jest.Mock).mockRejectedValue(new Error('offline'));
  await act(async () => {
    await client.refetchQueries({ queryKey: ['requests', 'mine'] });
  });

  // The rejection really happened. The cache records it — getQueryState says
  // 'error' — and the observer deliberately does not pass it on while it holds
  // data, which is the distinction this screen depends on and the reason the
  // assertions below are about what is drawn rather than about query state.
  expect(api.myRequests).toHaveBeenCalledTimes(2);
  expect(client.getQueryState(['requests', 'mine'])?.status).toBe('error');

  expect(screen.getByTestId('request-route')).toBeTruthy();
  expect(screen.getByTestId('request-progress')).toBeTruthy();
  expect(screen.queryByTestId('request-failed')).toBeNull();
  // And cancelling is still offered, because there is still something to cancel.
  expect(screen.getByTestId('cancel-request')).toBeTruthy();
});

// Matched, cancelled or expired: it left the open list on purpose, and the
// honest thing to say is that it is no longer being waited on.
test('a request no longer in the open list is reported as closed, not as no drivers', async () => {
  (api.myRequests as jest.Mock).mockResolvedValue({ requests: [] });

  render(wrap(<RequestScreen />));

  expect(await screen.findByTestId('request-closed')).toBeTruthy();
  expect(screen.queryByTestId('request-no-drivers')).toBeNull();
  expect(screen.queryByTestId('request-progress')).toBeNull();
});

// Cancelling needs something to cancel. The button used to be drawn in all
// four states, so a request that had already been matched or called off still
// offered "Ακύρωση αιτήματος" — a DELETE against something that is not open —
// and during loading it offered to cancel a request nobody could see yet.
test('nothing to cancel means no cancel button', async () => {
  (api.myRequests as jest.Mock).mockResolvedValue({ requests: [] });

  render(wrap(<RequestScreen />));

  expect(await screen.findByTestId('request-closed')).toBeTruthy();
  expect(screen.queryByTestId('cancel-request')).toBeNull();
  // Leaving is still the ordinary thing to do.
  expect(screen.getByTestId('request-back-home')).toBeTruthy();
});

test('a request that is open can still be cancelled', async () => {
  (api.myRequests as jest.Mock).mockResolvedValue({
    requests: [
      {
        id: 'req-1',
        origin_label: 'Αλεξανδρούπολη',
        destination_label: 'Ορεστιάδα',
        window_start: '2026-09-17T04:30:00Z',
        window_end: '2026-09-17T06:30:00Z',
        seats: 1,
        status: 'asking',
        drivers_asked: 1,
        drivers_declined: 0,
        asking_now: true,
        drivers_on_corridor: 1,
        created_at: '2026-09-16T06:00:00Z',
      },
    ],
  });

  render(wrap(<RequestScreen />));

  expect(await screen.findByTestId('cancel-request')).toBeTruthy();
});

// The one case that really is about supply, and the only one allowed to say so.
test('a request on a road nobody drives says exactly that', async () => {
  (api.myRequests as jest.Mock).mockResolvedValue({
    requests: [
      {
        id: 'req-1',
        origin_label: 'Αλεξανδρούπολη',
        destination_label: 'Ορεστιάδα',
        window_start: '2026-09-17T04:30:00Z',
        window_end: '2026-09-17T06:30:00Z',
        seats: 1,
        status: 'open',
        drivers_asked: 0,
        drivers_declined: 0,
        asking_now: false,
        drivers_on_corridor: 0,
        created_at: '2026-09-16T06:00:00Z',
      },
    ],
  });

  render(wrap(<RequestScreen />));

  await waitFor(() => expect(screen.getByTestId('request-route')).toBeTruthy());
  expect(screen.getByTestId('request-progress').props.children).toBe(el.home.requestNoDrivers);
});

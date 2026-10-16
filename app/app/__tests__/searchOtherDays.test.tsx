import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import SearchScreen from '../(tabs)/search';
import { api } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';

// Only today or tomorrow could be asked for, so a trip on Thursday waited
// until Wednesday (QA 2026-10-16, U1). A passenger can now pick a date, and
// say which other days would do as well.
const mockPush = jest.fn();
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush, replace: jest.fn() }),
  useLocalSearchParams: () => ({}),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: {
    places: jest.fn(),
    placeNear: jest.fn(),
    meetingPoints: jest.fn(),
    proposeMeetingPoint: jest.fn(),
    createRequest: jest.fn(),
    estimate: jest.fn(),
    liveMap: jest.fn(),
    supply: jest.fn(),
  },
  LUGGAGE_SIZES: ['none', 'cabin', 'suitcase', 'bulky'],
}));

jest.mock('../../src/push/reach', () => ({
  usePushReach: (select: (s: unknown) => unknown) => select({ on: true }),
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const pin = (label: string, lon: number, lat: number) => ({
  id: `node/${label}`,
  area: 'Δοκιμής',
  kind: 'town' as const,
  name: label,
  lon,
  lat,
});

const fuel = {
  id: 'node/77',
  place_id: 'node/Φέρες',
  kind: 'fuel',
  name: 'Βενζινάδικο στην εθνική',
  lon: 26.15,
  lat: 40.91,
};

beforeEach(() => {
  jest.clearAllMocks();
  (api.liveMap as jest.Mock).mockResolvedValue({ trips: [] });
  (api.estimate as jest.Mock).mockRejectedValue(new Error('offline'));
  (api.supply as jest.Mock).mockResolvedValue({ status: 'unknown' });
  (api.createRequest as jest.Mock).mockImplementation(async (b: { alternative_of?: string }) => ({
    request_id: b.alternative_of ? `alt-${(api.createRequest as jest.Mock).mock.calls.length}` : 'q1',
    asking: false,
  }));
  (api.places as jest.Mock).mockImplementation(async (q: string) => ({
    places: q.startsWith('φε') ? [pin('Φέρες', 26.17, 40.9)] : [pin('Αλεξανδρούπολη', 25.87, 40.84)],
  }));
  (api.meetingPoints as jest.Mock).mockImplementation(async (place: string) =>
    place === 'node/Φέρες' ? [fuel] : [],
  );
});

async function choose(field: string, typed: string, name: string) {
  fireEvent.changeText(screen.getByTestId(`${field}-input`), typed);
  await waitFor(() => screen.getByTestId(`${field}-node/${name}`));
  fireEvent.press(screen.getByTestId(`${field}-node/${name}`));
}

const dayOf = (iso: string) =>
  new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Athens' }).format(new Date(iso));

test('another day is a date further on', async () => {
  render(wrap(<SearchScreen />));
  await choose('from', 'φερ', 'Φέρες');
  await choose('to', 'αλεξ', 'Αλεξανδρούπολη');

  fireEvent.press(screen.getByTestId('when-day-other'));
  const dates = screen.getAllByTestId(/^when-date-/);
  const picked = dates[2].props.testID.replace('when-date-', '');
  fireEvent.press(dates[2]);
  fireEvent.press(screen.getByTestId('search-submit'));

  await waitFor(() => expect(api.createRequest).toHaveBeenCalled());
  const body = (api.createRequest as jest.Mock).mock.calls[0][0];
  expect(dayOf(body.window_start)).toBe(picked);
  expect(body.alternative_of).toBeUndefined();
});

test('other days that would do are asked for too, as alternatives of the first', async () => {
  render(wrap(<SearchScreen />));
  await choose('from', 'φερ', 'Φέρες');
  await choose('to', 'αλεξ', 'Αλεξανδρούπολη');

  fireEvent.press(screen.getByTestId('when-more'));
  const extras = screen.getAllByTestId(/^when-extra-/);
  const days = [extras[1], extras[3]].map((e) => e.props.testID.replace('when-extra-', ''));
  fireEvent.press(extras[1]);
  fireEvent.press(extras[3]);
  fireEvent.press(screen.getByTestId('search-submit'));

  await waitFor(() => expect(api.createRequest).toHaveBeenCalledTimes(3));
  const calls = (api.createRequest as jest.Mock).mock.calls.map((c) => c[0]);
  expect(calls[0].alternative_of).toBeUndefined();
  expect(calls.slice(1).map((b) => b.alternative_of)).toEqual(['q1', 'q1']);
  expect(calls.slice(1).map((b) => dayOf(b.window_start))).toEqual(days);
});

test('a day can be taken back off the list', async () => {
  render(wrap(<SearchScreen />));
  await choose('from', 'φερ', 'Φέρες');
  await choose('to', 'αλεξ', 'Αλεξανδρούπολη');

  fireEvent.press(screen.getByTestId('when-more'));
  const extra = screen.getAllByTestId(/^when-extra-/)[0];
  fireEvent.press(extra);
  fireEvent.press(screen.getAllByTestId(/^when-extra-/)[0]);
  fireEvent.press(screen.getByTestId('search-submit'));

  await waitFor(() => expect(api.createRequest).toHaveBeenCalled());
  await new Promise((r) => setTimeout(r, 50));
  expect(api.createRequest).toHaveBeenCalledTimes(1);
});

test('a day that could not be saved is said, and the rest are kept', async () => {
  (api.createRequest as jest.Mock).mockImplementation(async (b: { alternative_of?: string }) => {
    const n = (api.createRequest as jest.Mock).mock.calls.length;
    if (n === 2) throw new Error('offline');
    return { request_id: b.alternative_of ? `alt-${n}` : 'q1', asking: false };
  });
  render(wrap(<SearchScreen />));
  await choose('from', 'φερ', 'Φέρες');
  await choose('to', 'αλεξ', 'Αλεξανδρούπολη');

  fireEvent.press(screen.getByTestId('when-more'));
  fireEvent.press(screen.getAllByTestId(/^when-extra-/)[1]);
  fireEvent.press(screen.getAllByTestId(/^when-extra-/)[3]);
  fireEvent.press(screen.getByTestId('search-submit'));

  // The day after the failed one is still asked for.
  await waitFor(() => expect(api.createRequest).toHaveBeenCalledTimes(3));
  await screen.findByText(/1 μέρα|1 day/);
  expect(mockPush).not.toHaveBeenCalled();
  const failedDay = (api.createRequest as jest.Mock).mock.calls[1][0].window_start;

  // Pressing again sends only that day, as another day of the request already
  // saved. Sending the first day again would make a new request if a driver
  // had taken it meanwhile, and the passenger could be carried twice.
  fireEvent.press(screen.getByTestId('search-submit'));
  await waitFor(() => expect(api.createRequest).toHaveBeenCalledTimes(4));
  const retry = (api.createRequest as jest.Mock).mock.calls[3][0];
  expect(retry.alternative_of).toBe('q1');
  expect(retry.window_start).toBe(failedDay);
  await waitFor(() => expect(mockPush).toHaveBeenCalledWith('/request/q1'));
});

test('a day the server refuses for good is said, not offered again', async () => {
  (api.createRequest as jest.Mock).mockImplementation(async (b: { alternative_of?: string }) => {
    if (b.alternative_of) throw new ApiError(409, 'request_closed', 'closed');
    return { request_id: 'q1', asking: false };
  });
  render(wrap(<SearchScreen />));
  await choose('from', 'φερ', 'Φέρες');
  await choose('to', 'αλεξ', 'Αλεξανδρούπολη');

  fireEvent.press(screen.getByTestId('when-more'));
  fireEvent.press(screen.getAllByTestId(/^when-extra-/)[1]);
  fireEvent.press(screen.getByTestId('search-submit'));

  await screen.findByText(/δεν ψάχνει πια οδηγό/);
  expect(screen.queryByText(/αποθηκεύτηκε, αλλά/)).toBeNull();
});

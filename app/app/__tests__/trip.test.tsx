import { act, render as rtlRender, screen, fireEvent, waitFor, within } from '@testing-library/react-native';
import el from '../../src/i18n/el.json';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactElement } from 'react';

import { FlatList, StyleSheet } from 'react-native';
import TripScreen from '../trip/[id]';
import { colors } from '../../src/design/tokens';
import { api, type TripDetail } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';

const mockPush = jest.fn();
const mockBack = jest.fn();
const mockReplace = jest.fn();
let mockCanGoBack = true;
jest.mock('expo-router', () => ({
  useLocalSearchParams: () => ({ id: 'occ-1' }),
  useRouter: () => ({
    push: mockPush,
    replace: mockReplace,
    back: mockBack,
    canGoBack: () => mockCanGoBack,
  }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: {
    trip: jest.fn(),
    messages: jest.fn(),
    postMessage: jest.fn(),
    confirmTrip: jest.fn(),
    cancelTrip: jest.fn(),
    cancelBooking: jest.fn(),
    rateTrip: jest.fn(),
    here: jest.fn(),
  },
}));

// Their own screens have their own tests; here they only need to stay quiet.
jest.mock('../../src/components/LiveCard', () => ({ LiveCard: () => null }));
jest.mock('../../src/components/RepeatCard', () => ({ RepeatCard: () => null }));
// Its own test covers the filter; here only that the trip asks for its own.
jest.mock('../../src/components/PendingAsks', () => {
  const { Text } = require('react-native');
  return {
    PendingAsks: ({ occurrenceId }: { occurrenceId?: string }) => (
      <Text testID="pending-asks-for">{occurrenceId}</Text>
    ),
  };
});

// The confirm dialog answers yes at once, so a test reads as the tap it is.
jest.mock('../../src/components/confirm', () => ({
  confirm: ({ onConfirm }: { onConfirm: () => void }) => onConfirm(),
}));

let mockMe: { id: string } | null = { id: 'me' };
jest.mock('../../src/state/session', () => ({
  useSession: (pick: (s: { me: { id: string } | null }) => unknown) => pick({ me: mockMe }),
}));

const HOUR = 3_600_000;

function person(id: string, name: string, extra: Record<string, unknown> = {}) {
  return {
    id,
    name,
    photo_url: null,
    rating: null,
    facebook_url: null,
    instagram_url: null,
    can_rate: false,
    rated: false,
    ...extra,
  };
}

function tripOf(overrides: Partial<TripDetail> & Record<string, unknown> = {}): TripDetail {
  return {
    id: 'occ-1',
    role: 'passenger',
    depart_earliest: new Date(Date.now() + 2 * HOUR).toISOString(),
    depart_latest: new Date(Date.now() + 2.5 * HOUR).toISOString(),
    status: 'scheduled',
    route_km: 62,
    origin_label: 'Αλεξανδρούπολη',
    destination_label: 'Κομοτηνή',
    price_eur: 4.2,
    seats_free: 2,
    route_polyline: '',
    driver: person('drv', 'Γιώργος'),
    passengers: [person('me', 'Εγώ')],
    template_id: 't-1',
    weekday: 2,
    booking_id: 'bk-1',
    ends_at_port: false,
    confirmed_by_me: false,
    ...overrides,
  } as TripDetail;
}

function render(node: ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return { client, ...rtlRender(<QueryClientProvider client={client}>{node}</QueryClientProvider>) };
}

beforeEach(() => {
  jest.clearAllMocks();
  mockMe = { id: 'me' };
  mockCanGoBack = true;
  (api.messages as jest.Mock).mockResolvedValue({ messages: [] });
  (api.cancelBooking as jest.Mock).mockResolvedValue(undefined);
  (api.rateTrip as jest.Mock).mockResolvedValue(undefined);
  (api.confirmTrip as jest.Mock).mockResolvedValue(undefined);
});

// Giving back a seat is a way out, not what the screen is for. A solid red
// button above the chat was the heaviest thing on the page; it is a quiet one
// now, with the danger said by its words and its colour, and still confirmed.
test('the way out of a trip does not outweigh the trip', async () => {
  (api.trip as jest.Mock).mockResolvedValue(tripOf());
  render(<TripScreen />);

  const leave = await screen.findByTestId('trip-leave');
  const flat = StyleSheet.flatten(leave.props.style);
  expect(flat.backgroundColor).not.toBe(colors.danger);
});

describe('a passenger giving back their seat', () => {
  // The server has always let a passenger cancel; the screen only ever offered
  // it to drivers, so a passenger who could no longer go stranded the car.
  it('cancels their own booking, not the trip', async () => {
    (api.trip as jest.Mock).mockResolvedValue(tripOf());
    render(<TripScreen />);

    fireEvent.press(await screen.findByTestId('trip-leave'));

    await waitFor(() => expect(api.cancelBooking).toHaveBeenCalledWith('bk-1'));
    expect(api.cancelTrip).not.toHaveBeenCalled();
    // The trip is no longer theirs to look at: the server answers 404 from here.
    await waitFor(() => expect(mockBack).toHaveBeenCalled());
  });

  // Opened from a notification or a link there is nothing to go back to, and
  // staying put leaves them on a trip the server no longer shows them.
  it('goes home when there is nothing to go back to', async () => {
    mockCanGoBack = false;
    (api.trip as jest.Mock).mockResolvedValue(tripOf());
    render(<TripScreen />);

    fireEvent.press(await screen.findByTestId('trip-leave'));

    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith('/(tabs)'));
    expect(mockBack).not.toHaveBeenCalled();
  });

  it('says so when the car has already left', async () => {
    (api.trip as jest.Mock).mockResolvedValue(tripOf());
    (api.cancelBooking as jest.Mock).mockRejectedValue(new ApiError(409, 'departed', 'gone'));
    render(<TripScreen />);

    fireEvent.press(await screen.findByTestId('trip-leave'));

    expect(await screen.findByTestId('trip-notice')).toHaveTextContent(/ήδη ξεκινήσει/);
    expect(mockBack).not.toHaveBeenCalled();
  });

  it('is not offered on a cancelled trip', async () => {
    (api.trip as jest.Mock).mockResolvedValue(tripOf({ status: 'cancelled' }));
    render(<TripScreen />);

    expect(await screen.findByTestId('trip-status')).toHaveTextContent(/ακυρώθηκε/i);
    expect(screen.queryByTestId('trip-leave')).toBeNull();
    expect(screen.queryByTestId('trip-cancel')).toBeNull();
  });
});

describe('after the trip', () => {
  const past = {
    depart_earliest: new Date(Date.now() - 3 * HOUR).toISOString(),
    depart_latest: new Date(Date.now() - 2 * HOUR).toISOString(),
  };

  // A cancelled trip past its hour used to ask whether it had happened.
  it('asks nothing about a cancelled trip', async () => {
    (api.trip as jest.Mock).mockResolvedValue(tripOf({ ...past, status: 'cancelled' }));
    render(<TripScreen />);

    await screen.findByTestId('trip-status');
    expect(screen.queryByTestId('trip-confirm')).toBeNull();
    expect(screen.queryByTestId('rate-drv')).toBeNull();
  });

  // The server only accepts a confirmation once the far end of the window has
  // passed; offering it at the near end was a button that answered "not yet".
  it('does not offer confirmation inside the departure window', async () => {
    (api.trip as jest.Mock).mockResolvedValue(
      tripOf({
        depart_earliest: new Date(Date.now() - HOUR).toISOString(),
        depart_latest: new Date(Date.now() + HOUR).toISOString(),
      }),
    );
    render(<TripScreen />);

    await screen.findByTestId('trip-status');
    expect(screen.queryByTestId('trip-confirm')).toBeNull();
    expect(screen.queryByTestId('trip-leave')).toBeNull();
  });

  it('stops asking once you have confirmed', async () => {
    (api.trip as jest.Mock).mockResolvedValue(tripOf({ ...past, confirmed_by_me: true }));
    render(<TripScreen />);

    expect(await screen.findByTestId('trip-status')).toHaveTextContent(/επιβεβαίωσες/i);
    expect(screen.queryByTestId('trip-confirm')).toBeNull();
  });

  // Every rating used to be five stars, and a driver could only ever rate
  // whoever booked first.
  it('rates the chosen passenger with the chosen stars', async () => {
    (api.trip as jest.Mock).mockResolvedValue(
      tripOf({
        ...past,
        role: 'driver',
        booking_id: null,
        confirmed_by_me: true,
        driver: person('me', 'Εγώ'),
        passengers: [
          person('p-a', 'Άννα', { can_rate: true }),
          person('p-b', 'Βασίλης', { can_rate: true }),
        ],
      }),
    );
    render(<TripScreen />);

    fireEvent.press(await screen.findByTestId('rate-p-b-star-3'));
    fireEvent.press(screen.getByTestId('rate-p-b-send'));

    await waitFor(() => expect(api.rateTrip).toHaveBeenCalledWith('occ-1', 3, 'p-b'));
    // Rating one passenger leaves the other still to do.
    expect(screen.getByTestId('rate-p-a-send')).toBeTruthy();
  });

  it('sends nothing until a star is picked', async () => {
    (api.trip as jest.Mock).mockResolvedValue(
      tripOf({ ...past, confirmed_by_me: true, driver: person('drv', 'Γιώργος', { can_rate: true }) }),
    );
    render(<TripScreen />);

    fireEvent.press(await screen.findByTestId('rate-drv-send'));

    expect(api.rateTrip).not.toHaveBeenCalled();
  });

  it('treats "already rated" as done, not as a failure', async () => {
    (api.trip as jest.Mock).mockResolvedValue(
      tripOf({ ...past, confirmed_by_me: true, driver: person('drv', 'Γιώργος', { can_rate: true }) }),
    );
    (api.rateTrip as jest.Mock).mockRejectedValue(new ApiError(409, 'already_rated', 'done'));
    render(<TripScreen />);

    fireEvent.press(await screen.findByTestId('rate-drv-star-4'));
    fireEvent.press(screen.getByTestId('rate-drv-send'));

    expect(await screen.findByTestId('rate-drv-done')).toBeTruthy();
    expect(screen.queryByTestId('trip-notice')).toBeNull();
  });

  // Rating opens once both sides confirm, and the other side may do that
  // while this screen is open.
  it('offers the rating once the other side confirms, without leaving', async () => {
    jest.useFakeTimers();
    try {
      (api.trip as jest.Mock)
        .mockResolvedValueOnce(tripOf({ ...past, confirmed_by_me: true }))
        .mockResolvedValue(
          tripOf({ ...past, confirmed_by_me: true, driver: person('drv', 'Γιώργος', { can_rate: true }) }),
        );
      render(<TripScreen />);
      await screen.findByTestId('trip-status');
      expect(screen.queryByTestId('rate-drv-send')).toBeNull();

      await act(async () => {
        jest.advanceTimersByTime(31_000);
      });

      expect(await screen.findByTestId('rate-drv-send')).toBeTruthy();
    } finally {
      jest.useRealTimers();
    }
  });

  it('shows nothing to rate for somebody already rated', async () => {
    (api.trip as jest.Mock).mockResolvedValue(
      tripOf({ ...past, confirmed_by_me: true, driver: person('drv', 'Γιώργος', { rated: true }) }),
    );
    render(<TripScreen />);

    expect(await screen.findByTestId('rate-drv-done')).toBeTruthy();
    expect(screen.queryByTestId('rate-drv-send')).toBeNull();
  });
});

describe('who you are riding with', () => {
  // The block and report screen existed and nothing led to it.
  it('opens the driver, with their name, from the passenger side', async () => {
    (api.trip as jest.Mock).mockResolvedValue(tripOf());
    render(<TripScreen />);

    fireEvent.press(await screen.findByTestId('person-drv'));

    expect(mockPush).toHaveBeenCalledWith({
      pathname: '/user/[id]',
      params: { id: 'drv', name: 'Γιώργος' },
    });
  });

  it('does not open yourself', async () => {
    (api.trip as jest.Mock).mockResolvedValue(
      tripOf({
        role: 'driver',
        booking_id: null,
        driver: person('me', 'Εγώ'),
        passengers: [person('p-a', 'Άννα')],
      }),
    );
    render(<TripScreen />);

    fireEvent.press(await screen.findByTestId('person-p-a'));
    expect(mockPush).toHaveBeenCalledWith({
      pathname: '/user/[id]',
      params: { id: 'p-a', name: 'Άννα' },
    });
    expect(screen.queryByTestId('person-me')).toBeNull();
  });

  // Who is in the car, for everybody in it: a passenger used to see only the
  // driver, and found out who else was coming at the stop.
  it('shows a passenger who else is in the car, and opens them', async () => {
    (api.trip as jest.Mock).mockResolvedValue(
      tripOf({ passengers: [person('me', 'Εγώ'), person('p-b', 'Βασίλης')] }),
    );
    render(<TripScreen />);

    expect(await screen.findByTestId('passenger-p-b')).toBeTruthy();
    expect(screen.getByTestId('passenger-me')).toBeTruthy();
    expect(screen.getByText(new RegExp(`Εγώ · ${el.trip.you}`))).toBeTruthy();

    fireEvent.press(screen.getByTestId('person-p-b'));
    expect(mockPush).toHaveBeenCalledWith({
      pathname: '/user/[id]',
      params: { id: 'p-b', name: 'Βασίλης' },
    });
  });
});

describe('the chat', () => {
  // A message that failed to send used to vanish without a word: the box
  // stayed full and nothing said it had not gone.
  it('says so when a message does not go, and keeps it', async () => {
    (api.trip as jest.Mock).mockResolvedValue(tripOf());
    (api.postMessage as jest.Mock).mockRejectedValue(new Error('offline'));
    render(<TripScreen />);

    fireEvent.changeText(await screen.findByTestId('message-input'), 'Στο περίπτερο;');
    fireEvent.press(screen.getByTestId('message-send'));

    expect(await screen.findByTestId('message-failed')).toBeTruthy();
    expect(screen.getByTestId('message-input').props.value).toBe('Στο περίπτερο;');
  });

  it('clears the failure once a message goes', async () => {
    (api.trip as jest.Mock).mockResolvedValue(tripOf());
    (api.postMessage as jest.Mock)
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValue({ id: 'm-1' });
    render(<TripScreen />);

    fireEvent.changeText(await screen.findByTestId('message-input'), 'Γεια');
    fireEvent.press(screen.getByTestId('message-send'));
    await screen.findByTestId('message-failed');
    // The failure can render a beat before the send stops counting as pending;
    // a tap in between lands on a disabled button.
    await waitFor(() =>
      expect(screen.getByTestId('message-send')).toBeEnabled(),
    );
    fireEvent.press(screen.getByTestId('message-send'));

    await waitFor(() => expect(screen.queryByTestId('message-failed')).toBeNull(), {
      timeout: 5000,
    });
    expect(screen.getByTestId('message-input').props.value).toBe('');
  });

  // The newest message used to arrive out of sight: no scroll to it and no
  // sign that it had come.
  describe('new messages', () => {
    const msg = (n: number, sender = 'drv') => ({
      id: `m${n}`,
      sender_id: sender,
      sender_name: 'Γιώργος',
      body: `μήνυμα ${n}`,
      created_at: new Date().toISOString(),
    });
    const scrollTo = (y: number) =>
      fireEvent.scroll(screen.getByTestId('message-list'), {
        nativeEvent: {
          contentOffset: { x: 0, y },
          layoutMeasurement: { width: 300, height: 600 },
          contentSize: { width: 300, height: 2000 },
        },
      });
    const refetchWith = async (client: QueryClient, messages: unknown[]) => {
      (api.messages as jest.Mock).mockResolvedValue({ messages });
      await act(async () => {
        await client.invalidateQueries({ queryKey: ['trips', 'occ-1', 'messages'] });
      });
    };

    it('points at a new message when the reader has scrolled up, and goes to it on press', async () => {
      (api.trip as jest.Mock).mockResolvedValue(tripOf());
      (api.messages as jest.Mock).mockResolvedValue({ messages: [msg(1)] });
      const scrollEnd = jest.spyOn(FlatList.prototype, 'scrollToEnd').mockImplementation(() => {});
      const { client } = render(<TripScreen />);
      await screen.findByText('μήνυμα 1');

      scrollTo(0);
      await refetchWith(client, [msg(1), msg(2)]);

      const pill = await screen.findByTestId('new-message');
      expect(within(pill).getByText(el.trip.newMessage)).toBeTruthy();
      expect(scrollEnd).not.toHaveBeenCalled();

      fireEvent.press(pill);
      expect(screen.queryByTestId('new-message')).toBeNull();
      await waitFor(() => expect(scrollEnd).toHaveBeenCalled());
      scrollEnd.mockRestore();
    });

    it('hides the pointer once the reader reaches the bottom', async () => {
      (api.trip as jest.Mock).mockResolvedValue(tripOf());
      (api.messages as jest.Mock).mockResolvedValue({ messages: [msg(1)] });
      const { client } = render(<TripScreen />);
      await screen.findByText('μήνυμα 1');

      scrollTo(0);
      await refetchWith(client, [msg(1), msg(2)]);
      await screen.findByTestId('new-message');

      scrollTo(1400);
      expect(screen.queryByTestId('new-message')).toBeNull();
    });

    it('follows a new message down when the reader is at the bottom', async () => {
      (api.trip as jest.Mock).mockResolvedValue(tripOf());
      (api.messages as jest.Mock).mockResolvedValue({ messages: [msg(1)] });
      const scrollEnd = jest.spyOn(FlatList.prototype, 'scrollToEnd').mockImplementation(() => {});
      const { client } = render(<TripScreen />);
      await screen.findByText('μήνυμα 1');

      scrollTo(1400);
      await refetchWith(client, [msg(1), msg(2)]);

      await waitFor(() => expect(scrollEnd).toHaveBeenCalled());
      expect(screen.queryByTestId('new-message')).toBeNull();
      scrollEnd.mockRestore();
    });

    it('goes to the end after the reader sends, even from further up', async () => {
      (api.trip as jest.Mock).mockResolvedValue(tripOf());
      (api.messages as jest.Mock).mockResolvedValue({ messages: [msg(1)] });
      (api.postMessage as jest.Mock).mockResolvedValue(undefined);
      const scrollEnd = jest.spyOn(FlatList.prototype, 'scrollToEnd').mockImplementation(() => {});
      const { client } = render(<TripScreen />);
      await screen.findByText('μήνυμα 1');

      scrollTo(0);
      fireEvent.changeText(screen.getByTestId('message-input'), 'Έρχομαι');
      fireEvent.press(screen.getByTestId('message-send'));
      await waitFor(() => expect(api.postMessage).toHaveBeenCalled());
      await refetchWith(client, [msg(1), msg(2, 'me')]);

      await waitFor(() => expect(scrollEnd).toHaveBeenCalled());
      expect(screen.queryByTestId('new-message')).toBeNull();
      scrollEnd.mockRestore();
    });
  });
});

// On a drive a passenger says when they are at the pickup, once, and the
// screen then says the driver knows.
test('a passenger on a drive says «Είμαι εδώ»', async () => {
  (api.here as jest.Mock).mockResolvedValue(undefined);
  (api.trip as jest.Mock).mockResolvedValue(
    tripOf({
      drive_set_at: new Date().toISOString(),
      passengers: [person('me', 'Εγώ', { arrived_at: null })],
    }),
  );
  render(<TripScreen />);
  fireEvent.press(await screen.findByTestId('here'));
  await waitFor(() => expect(api.here).toHaveBeenCalledWith('occ-1'));
  expect(await screen.findByTestId('here-done')).toBeTruthy();
});

test('there is no «Είμαι εδώ» without a drive', async () => {
  (api.trip as jest.Mock).mockResolvedValue(tripOf({ passengers: [person('me', 'Εγώ')] }));
  render(<TripScreen />);
  await screen.findByTestId('route-glyph');
  expect(screen.queryByTestId('here')).toBeNull();
});

// On the road the trip is under way, not over: nobody is asked «Έγινε;»
// a minute after setting off, only after «Τέλος».
test('a driver on the road is not asked whether the trip happened', async () => {
  const left = new Date(Date.now() - 60_000).toISOString();
  (api.trip as jest.Mock).mockResolvedValue(
    tripOf({
      role: 'driver',
      depart_earliest: left,
      depart_latest: left,
      drive_set_at: left,
      drive_ended_at: null,
      passengers: [],
    }),
  );
  render(<TripScreen />);
  await screen.findByTestId('route-glyph');
  expect(screen.queryByTestId('trip-confirm')).toBeNull();
});

// A booking can hold more than one seat. Counting passengers instead of seats
// drew a car with a seat missing.
test('the seat pips count booked seats, not passengers', async () => {
  (api.trip as jest.Mock).mockResolvedValue(
    tripOf({ seats_free: 1, passengers: [person('me', 'Εγώ', { seats: 2 })] }),
  );
  render(<TripScreen />);

  const pips = await screen.findByTestId('seat-pips');
  expect(pips.props.accessibilityLabel).toBe('1/3');
});

// QA U F3: the screen says where and in what order. The ticket has the car's
// two ends; the stops card has everybody picked up and dropped in between.
describe('stops card', () => {
  const at = (h: number, m: number) => new Date(Date.UTC(2030, 0, 15, h, m)).toISOString();
  const stop = (extra: Record<string, unknown>) => ({
    kind: 'pickup',
    mine: false,
    passenger_id: null,
    name: null,
    label: null,
    point: null,
    at: null,
    seats: null,
    luggage: null,
    luggage_count: null,
    ...extra,
  });

  test('the driver sees each stop in order with name, place, seats and luggage', async () => {
    (api.trip as jest.Mock).mockResolvedValue(
      tripOf({
        role: 'driver',
        booking_id: null,
        passengers: [person('p1', 'Μαρία'), person('p2', 'Νίκος')],
        stops: [
          stop({ kind: 'pickup', passenger_id: 'p1', name: 'Μαρία', label: 'Λιμάνι', at: at(5, 30), seats: 2, luggage_count: 1 }),
          stop({ kind: 'pickup', passenger_id: 'p2', name: 'Νίκος', label: 'Πλατεία', at: at(5, 40), seats: 1, luggage_count: 0 }),
          stop({ kind: 'dropoff', passenger_id: 'p1', name: 'Μαρία', label: 'Κομοτηνή Κέντρο', at: at(6, 20), seats: 2, luggage_count: 1 }),
        ],
      } as never),
    );
    render(<TripScreen />);

    expect(await screen.findByText('Στάσεις')).toBeTruthy();
    const first = screen.getByTestId('stop-0');
    expect(within(first).getByText(/07:30/)).toBeTruthy();
    expect(within(first).getByText(/Παραλαβή/)).toBeTruthy();
    expect(within(first).getByText(/Μαρία · Λιμάνι · 2 θέσεις · 1 αποσκευή/)).toBeTruthy();
    const second = screen.getByTestId('stop-1');
    expect(within(second).getByText(/Νίκος · Πλατεία · 1 θέση$/)).toBeTruthy();
    const third = screen.getByTestId('stop-2');
    expect(within(third).getByText(/Αποβίβαση/)).toBeTruthy();
  });

  test('a passenger sees their own place and only «Άλλος επιβάτης» for the rest', async () => {
    (api.trip as jest.Mock).mockResolvedValue(
      tripOf({
        stops: [
          stop({ kind: 'pickup', mine: false }),
          stop({ kind: 'pickup', mine: true, passenger_id: 'me', name: 'Εγώ', label: 'Αλεξανδρούπολη Λιμάνι', at: at(5, 30) }),
          stop({ kind: 'dropoff', mine: false }),
        ],
      } as never),
    );
    render(<TripScreen />);

    await screen.findByText('Στάσεις');
    expect(within(screen.getByTestId('stop-1')).getByText(/Εσύ · Αλεξανδρούπολη Λιμάνι/)).toBeTruthy();
    expect(within(screen.getByTestId('stop-0')).getByText('Άλλος επιβάτης')).toBeTruthy();
    expect(within(screen.getByTestId('stop-2')).getByText('Άλλος επιβάτης')).toBeTruthy();
  });

  test('no card without stops', async () => {
    (api.trip as jest.Mock).mockResolvedValue(tripOf({ stops: [] } as never));
    render(<TripScreen />);
    await screen.findByTestId('trip-leave');
    expect(screen.queryByText('Στάσεις')).toBeNull();
  });

  test('no card when the server sends no stops at all', async () => {
    (api.trip as jest.Mock).mockResolvedValue(tripOf());
    render(<TripScreen />);
    await screen.findByTestId('trip-leave');
    expect(screen.queryByText('Στάσεις')).toBeNull();
  });
});

// The dots alone did not say whether they counted free or taken seats.
test('free seats are said in words beside the dots', async () => {
  (api.trip as jest.Mock).mockResolvedValue(tripOf({ seats_free: 2 }));
  const { unmount } = render(<TripScreen />);
  expect(await screen.findByText('2 ελεύθερες θέσεις')).toBeTruthy();
  unmount();

  (api.trip as jest.Mock).mockResolvedValue(tripOf({ seats_free: 1 }));
  render(<TripScreen />);
  expect(await screen.findByText('1 ελεύθερη θέση')).toBeTruthy();
});

// QA F4: the price on a ticket is per seat.
test('a passenger holding two seats sees the price per seat and the total', async () => {
  (api.trip as jest.Mock).mockResolvedValue(
    tripOf({ price_eur: 2.5, seats: 2, passengers: [person('me', 'Εγώ', { seats: 2 })] }),
  );
  render(<TripScreen />);

  expect(await screen.findByText(/ανά θέση · σύνολο 5,00.€/)).toBeTruthy();
});

test('one seat is a bare price on the ticket', async () => {
  (api.trip as jest.Mock).mockResolvedValue(tripOf({ price_eur: 2.5, seats: 1 }));
  render(<TripScreen />);

  await screen.findByTestId('seat-pips');
  expect(screen.queryByText(/ανά θέση/)).toBeNull();
});

// QA F8: a driver on the trip screen has to see a new ask for it.
describe('a question waiting on the driver', () => {
  it('is on the driver\'s trip, for this trip', async () => {
    (api.trip as jest.Mock).mockResolvedValue(tripOf({ role: 'driver', booking_id: null, price_eur: 0 }));
    render(<TripScreen />);

    expect(await screen.findByTestId('pending-asks-for')).toHaveTextContent('occ-1');
  });

  it('is not on a passenger\'s trip', async () => {
    (api.trip as jest.Mock).mockResolvedValue(tripOf());
    render(<TripScreen />);

    await screen.findByTestId('trip-leave');
    expect(screen.queryByTestId('pending-asks-for')).toBeNull();
  });
});

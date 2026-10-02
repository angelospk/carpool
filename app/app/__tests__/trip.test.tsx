import { act, render as rtlRender, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactElement } from 'react';

import TripScreen from '../trip/[id]';
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
  },
}));

// Their own screens have their own tests; here they only need to stay quiet.
jest.mock('../../src/components/LiveCard', () => ({ LiveCard: () => null }));
jest.mock('../../src/components/RepeatCard', () => ({ RepeatCard: () => null }));

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
    fireEvent.press(screen.getByTestId('message-send'));

    await waitFor(() => expect(screen.queryByTestId('message-failed')).toBeNull());
    expect(screen.getByTestId('message-input').props.value).toBe('');
  });
});

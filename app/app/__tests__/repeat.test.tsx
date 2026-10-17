import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { RepeatCard } from '../../src/components/RepeatCard';
import { ApiError } from '../../src/api/client';
import { api, type Arrangement, type TripDetail } from '../../src/api/endpoints';

jest.mock('../../src/api/endpoints', () => ({
  api: {
    arrangements: jest.fn(),
    repeatBooking: jest.fn(),
    approveArrangement: jest.fn(),
    declineArrangement: jest.fn(),
    cancelArrangement: jest.fn(),
  },
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const trip = {
  role: 'passenger',
  template_id: 'tpl-1',
  weekday: 2,
  booking_id: 'bk-1',
} as unknown as TripDetail;

const arrangement: Arrangement = {
  id: 'arr-1',
  template_id: 'tpl-1',
  weekday: 2,
  seats: 1,
  price_eur: 3.2,
  status: 'pending',
  until_date: '2026-08-22',
  driver_id: 'd1',
  passenger_id: 'p1',
  passenger_name: 'Μαρία',
  origin_label: 'Αλεξ/πολη',
  destination_label: 'Φέρες',
  weeks_booked: 0,
};

beforeEach(() => {
  jest.clearAllMocks();
  (api.arrangements as jest.Mock).mockResolvedValue({ arrangements: [] });
});

// QA F7: a one-off route does not run again, so there is nothing to repeat.
test('a route that does not repeat offers no repeat card', async () => {
  const view = render(wrap(<RepeatCard trip={{ ...trip, repeats: false } as TripDetail} />));

  await waitFor(() => expect(api.arrangements).toHaveBeenCalled());
  expect(screen.queryByTestId('repeat-switch')).toBeNull();
  expect(screen.queryByTestId('repeat-state')).toBeNull();
  expect(view.toJSON()).toBeNull();
});

test('a route that repeats offers the switch', async () => {
  render(wrap(<RepeatCard trip={{ ...trip, repeats: true } as TripDetail} />));

  expect(await screen.findByTestId('repeat-switch')).toBeTruthy();
});

test('the passenger turns the switch on and the driver is asked once', async () => {
  (api.repeatBooking as jest.Mock).mockResolvedValue({ arrangement_id: 'arr-1' });

  render(wrap(<RepeatCard trip={trip} />));
  fireEvent(screen.getByTestId('repeat-switch'), 'valueChange', true);

  await waitFor(() => expect(api.repeatBooking).toHaveBeenCalledWith('bk-1'));
});

// A switch that is on while the driver has not answered would promise a seat
// nobody is holding, so the state has to be said in words.
test('a pending arrangement says it is waiting rather than looking settled', async () => {
  (api.arrangements as jest.Mock).mockResolvedValue({ arrangements: [arrangement] });

  render(wrap(<RepeatCard trip={trip} />));

  await waitFor(() =>
    expect(String(screen.getByTestId('repeat-state').props.children)).toContain('Περιμένει'),
  );
});

test('an active arrangement says how many weeks it actually holds', async () => {
  (api.arrangements as jest.Mock).mockResolvedValue({
    arrangements: [{ ...arrangement, status: 'active', weeks_booked: 3 }],
  });

  render(wrap(<RepeatCard trip={trip} />));

  await waitFor(() =>
    expect(String(screen.getByTestId('repeat-state').props.children)).toContain('3'),
  );
});

test('the driver approves with a button, not a switch', async () => {
  (api.arrangements as jest.Mock).mockResolvedValue({ arrangements: [arrangement] });
  (api.approveArrangement as jest.Mock).mockResolvedValue(undefined);

  render(wrap(<RepeatCard trip={{ ...trip, role: 'driver' } as TripDetail} />));

  await waitFor(() => expect(screen.getByTestId('repeat-approve')).toBeTruthy());
  expect(screen.queryByTestId('repeat-switch')).toBeNull();

  fireEvent.press(screen.getByTestId('repeat-approve'));
  await waitFor(() => expect(api.approveArrangement).toHaveBeenCalledWith('arr-1'));
});

// The refusal names the day, because "no" without a date leaves the driver
// guessing which Tuesday to fix.
test('a full week is reported with the date that would not fit', async () => {
  (api.arrangements as jest.Mock).mockResolvedValue({ arrangements: [arrangement] });
  (api.approveArrangement as jest.Mock).mockRejectedValue(
    new ApiError(409, 'week_full', 'no seat', { code: 'week_full', dates: ['2026-08-11'] }),
  );

  render(wrap(<RepeatCard trip={{ ...trip, role: 'driver' } as TripDetail} />));
  await waitFor(() => expect(screen.getByTestId('repeat-approve')).toBeTruthy());
  fireEvent.press(screen.getByTestId('repeat-approve'));

  await waitFor(() =>
    expect(String(screen.getByTestId('repeat-notice').props.children)).toContain('2026-08-11'),
  );
});

// The driver has no booking of their own, so there is nothing for them to start.
// An empty card headed "repeats every Tuesday" would read as a setting they had
// somehow already made, so the whole thing stays off the screen.
test('the driver is shown nothing at all until a passenger asks', () => {
  const view = render(wrap(<RepeatCard trip={{ ...trip, role: 'driver' } as TripDetail} />));

  expect(view.toJSON()).toBeNull();
});

test('a passenger with no booking on this trip is shown nothing either', () => {
  const view = render(wrap(<RepeatCard trip={{ ...trip, booking_id: null } as TripDetail} />));

  expect(view.toJSON()).toBeNull();
});

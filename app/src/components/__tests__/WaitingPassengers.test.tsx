import { act, render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { WaitingPassengers, WeekendDepartures } from '../WaitingPassengers';
import { ApiError } from '../../api/client';
import { api, type WaitingPassenger } from '../../api/endpoints';

jest.mock('../../api/endpoints', () => ({
  api: { waiting: jest.fn(), offerSeat: jest.fn(), upcomingTrips: jest.fn() },
}));
jest.mock('../../state/session', () => ({
  useSession: (select: (s: unknown) => unknown) => select({ status: 'signedIn' }),
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const person = (id: string, over: Partial<WaitingPassenger> = {}): WaitingPassenger => ({
  request_id: id,
  passenger_name: 'Ελένη',
  seats: 1,
  luggage: 'none',
  luggage_count: 0,
  window_start: '2026-10-16T12:00:00Z', // 15:00 in Athens
  window_end: '2026-10-16T20:59:00Z', // 23:59
  origin_label: 'Αλεξανδρούπολη',
  destination_label: 'Θεσσαλονίκη',
  ...over,
});

const FRIDAY = '2026-10-16T12:00:00Z';

beforeEach(() => {
  jest.clearAllMocks();
  (api.waiting as jest.Mock).mockResolvedValue({ waiting: [person('r1'), person('r2', { passenger_name: 'Νίκος', seats: 2 })] });
  (api.offerSeat as jest.Mock).mockResolvedValue({ ask_id: 'a1' });
});

test('says how many wait on this road, on which day, and asks', async () => {
  render(wrap(<WaitingPassengers occurrenceId="occ-1" departs={FRIDAY} />));

  expect(await screen.findByTestId('waiting-occ-1-title')).toHaveTextContent(
    '2 άτομα περιμένουν σε αυτόν τον δρόμο την Παρασκευή. Τους παίρνεις;',
  );
  expect(api.waiting).toHaveBeenCalledWith('occ-1');
});

test('each person has a name, seats, bags and the hours they can go', async () => {
  (api.waiting as jest.Mock).mockResolvedValue({
    waiting: [person('r1', { seats: 2, luggage: 'suitcase', luggage_count: 1 })],
  });
  render(wrap(<WaitingPassengers occurrenceId="occ-1" departs={FRIDAY} />));

  const row = await screen.findByTestId('waiting-row-r1');
  expect(row).toHaveTextContent(/Ελένη/);
  expect(row).toHaveTextContent(/2 θέσεις/);
  expect(row).toHaveTextContent(/1 × Κανονική βαλίτσα/);
  expect(row).toHaveTextContent(/15:00–23:59/);
});

test('«Ναι» offers the seat and the row says so', async () => {
  render(wrap(<WaitingPassengers occurrenceId="occ-1" departs={FRIDAY} />));

  fireEvent.press(await screen.findByTestId('waiting-yes-r1'));

  await waitFor(() => expect(api.offerSeat).toHaveBeenCalledWith('occ-1', 'r1'));
  expect(await screen.findByTestId('waiting-offered-r1')).toHaveTextContent('Του/Της το προτείναμε');
  // The other person is still waiting for an answer.
  expect(screen.getByTestId('waiting-yes-r2')).toBeTruthy();
});

test('a person already offered the seat reads as offered', async () => {
  (api.offerSeat as jest.Mock).mockRejectedValue(new ApiError(409, 'already_offered', 'x'));
  render(wrap(<WaitingPassengers occurrenceId="occ-1" departs={FRIDAY} />));

  fireEvent.press(await screen.findByTestId('waiting-yes-r1'));

  expect(await screen.findByTestId('waiting-offered-r1')).toBeTruthy();
});

test('a refusal is said on the row and the button stays', async () => {
  (api.offerSeat as jest.Mock).mockRejectedValue(new ApiError(409, 'too_late', 'x'));
  render(wrap(<WaitingPassengers occurrenceId="occ-1" departs={FRIDAY} />));

  fireEvent.press(await screen.findByTestId('waiting-yes-r1'));

  expect(await screen.findByText('Η διαδρομή φεύγει πολύ σύντομα για να κάνεις πρόταση.')).toBeTruthy();
  expect(screen.getByTestId('waiting-yes-r1')).toBeTruthy();
});

test('nobody waiting draws nothing', async () => {
  (api.waiting as jest.Mock).mockResolvedValue({ waiting: [] });
  render(wrap(<WaitingPassengers occurrenceId="occ-1" departs={FRIDAY} />));

  await waitFor(() => expect(api.waiting).toHaveBeenCalled());
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
  expect(screen.queryByTestId('waiting-occ-1')).toBeNull();
});

describe('after publishing', () => {
  // Far enough ahead to be upcoming whenever this runs: 2099-01-02 is a
  // Friday, 2099-01-05 a Monday.
  const trip = (id: string, at: string, over: Record<string, unknown> = {}) => ({
    id,
    role: 'driver',
    depart_earliest: at,
    depart_latest: at,
    status: 'scheduled',
    route_km: 300,
    origin_label: 'Αλεξανδρούπολη',
    destination_label: 'Θεσσαλονίκη',
    price_eur: 8,
    seats_free: 3,
    ...over,
  });

  test('asks about the weekend departures of this route only', async () => {
    (api.upcomingTrips as jest.Mock).mockResolvedValue({
      trips: [
        trip('fri', '2099-01-02T12:00:00Z'),
        trip('mon', '2099-01-05T12:00:00Z'),
        trip('other-route', '2099-01-02T12:00:00Z', { destination_label: 'Κομοτηνή' }),
        trip('as-passenger', '2099-01-02T12:00:00Z', { role: 'passenger' }),
      ],
    });
    render(wrap(<WeekendDepartures from="Αλεξανδρούπολη" to="Θεσσαλονίκη" />));

    await waitFor(() => expect(api.waiting).toHaveBeenCalledTimes(1));
    expect(api.waiting).toHaveBeenCalledWith('fri');
    expect(await screen.findByTestId('waiting-fri')).toBeTruthy();
  });

  test('a route with no weekend departure asks nobody', async () => {
    (api.upcomingTrips as jest.Mock).mockResolvedValue({ trips: [trip('mon', '2099-01-05T12:00:00Z')] });
    render(wrap(<WeekendDepartures from="Αλεξανδρούπολη" to="Θεσσαλονίκη" />));

    await waitFor(() => expect(api.upcomingTrips).toHaveBeenCalled());
    await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
    expect(api.waiting).not.toHaveBeenCalled();
    expect(screen.queryByTestId('weekend-departures')).toBeNull();
  });
});

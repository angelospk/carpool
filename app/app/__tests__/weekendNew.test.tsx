import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import NewWeekendPlan from '../weekend/new';
import { ApiError } from '../../src/api/client';
import { api } from '../../src/api/endpoints';

const mockReplace = jest.fn();
jest.mock('expo-router', () => ({
  useRouter: () => ({ replace: mockReplace, push: jest.fn() }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { createWeekendPlan: jest.fn(), places: jest.fn() },
  LUGGAGE_SIZES: ['none', 'cabin', 'suitcase', 'bulky'],
}));

const pin = (label: string, lon: number, lat: number) => ({
  id: `node/${label}`,
  area: 'Δοκιμής',
  kind: 'town' as const,
  name: label,
  lon,
  lat,
});

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false }, queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

beforeEach(() => {
  jest.clearAllMocks();
  (api.places as jest.Mock).mockResolvedValue({
    places: [pin('Αλεξανδρούπολη', 25.87, 40.84), pin('Θεσσαλονίκη', 22.94, 40.64)],
  });
  (api.createWeekendPlan as jest.Mock).mockResolvedValue({ id: 'plan-1' });
});

async function pickEnds() {
  fireEvent.changeText(screen.getByTestId('weekend-from-input'), 'αλεξ');
  await waitFor(() => screen.getByTestId('weekend-from-node/Αλεξανδρούπολη'));
  fireEvent.press(screen.getByTestId('weekend-from-node/Αλεξανδρούπολη'));

  fireEvent.changeText(screen.getByTestId('weekend-to-input'), 'θεσ');
  await waitFor(() => screen.getByTestId('weekend-to-node/Θεσσαλονίκη'));
  fireEvent.press(screen.getByTestId('weekend-to-node/Θεσσαλονίκη'));
}

test('saving is shut until both ends are chosen', async () => {
  render(wrap(<NewWeekendPlan />));

  expect(screen.getByTestId('weekend-submit')).toBeDisabled();
  expect(screen.getByTestId('weekend-needs-places')).toBeTruthy();
  await pickEnds();
  expect(screen.getByTestId('weekend-submit')).toBeEnabled();
});

test('the defaults are Friday 15:00, one seat, every weekend, no return', async () => {
  render(wrap(<NewWeekendPlan />));
  await pickEnds();

  fireEvent.press(screen.getByTestId('weekend-submit'));

  await waitFor(() => expect(api.createWeekendPlan).toHaveBeenCalledTimes(1));
  expect(api.createWeekendPlan).toHaveBeenCalledWith({
    origin_lon: 25.87,
    origin_lat: 40.84,
    destination_lon: 22.94,
    destination_lat: 40.64,
    origin_label: 'Αλεξανδρούπολη',
    destination_label: 'Θεσσαλονίκη',
    depart_dow: 5,
    depart_time: '15:00',
    seats: 1,
    luggage: 'none',
    luggage_count: 0,
    every_week: true,
    client_key: expect.stringMatching(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/),
  });
  // Back home.
  await waitFor(() => expect(mockReplace).toHaveBeenCalledWith('/(tabs)'));
});

test('what the passenger chose is what is sent: Sunday return, two seats, a suitcase, once', async () => {
  render(wrap(<NewWeekendPlan />));
  await pickEnds();

  fireEvent.press(screen.getByTestId('weekend-out-day-6'));
  fireEvent.changeText(screen.getByTestId('weekend-out-time'), '0930');
  fireEvent(screen.getByTestId('weekend-out-time'), 'blur');
  fireEvent.press(screen.getByTestId('weekend-return-day-0'));
  fireEvent.changeText(screen.getByTestId('weekend-return-time'), '19:15');
  fireEvent.press(screen.getByTestId('weekend-seats-plus'));
  fireEvent.press(screen.getByTestId('weekend-luggage-suitcase'));
  fireEvent.press(screen.getByTestId('weekend-once'));

  fireEvent.press(screen.getByTestId('weekend-submit'));

  await waitFor(() => expect(api.createWeekendPlan).toHaveBeenCalledTimes(1));
  expect(api.createWeekendPlan).toHaveBeenCalledWith(
    expect.objectContaining({
      depart_dow: 6,
      depart_time: '09:30',
      return_dow: 0,
      return_time: '19:15',
      seats: 2,
      luggage: 'suitcase',
      luggage_count: 1,
      every_week: false,
    }),
  );
});

test('a time that is not a time shuts saving, and says so once left', async () => {
  render(wrap(<NewWeekendPlan />));
  await pickEnds();

  fireEvent.changeText(screen.getByTestId('weekend-out-time'), '25:99');
  fireEvent(screen.getByTestId('weekend-out-time'), 'blur');

  expect(screen.getByTestId('weekend-out-time-error')).toBeTruthy();
  expect(screen.getByTestId('weekend-submit')).toBeDisabled();
});

test('a return before the outbound is refused before it is sent', async () => {
  render(wrap(<NewWeekendPlan />));
  await pickEnds();

  // Friday 15:00 out, Friday 14:00 back.
  fireEvent.press(screen.getByTestId('weekend-return-day-5'));
  fireEvent.changeText(screen.getByTestId('weekend-return-time'), '14:00');

  expect(screen.getByTestId('weekend-return-error')).toHaveTextContent(/μετά την αναχώρηση/);
  expect(screen.getByTestId('weekend-submit')).toBeDisabled();
});

test('a refusal from the server is shown and the passenger stays', async () => {
  (api.createWeekendPlan as jest.Mock).mockRejectedValue(new ApiError(400, 'outside_country', 'x'));
  render(wrap(<NewWeekendPlan />));
  await pickEnds();

  fireEvent.press(screen.getByTestId('weekend-submit'));

  expect(await screen.findByTestId('weekend-outcome')).toHaveTextContent(/Ελλάδα/);
  expect(mockReplace).not.toHaveBeenCalled();
});

test('a save sent again carries the same key, so the server makes one plan', async () => {
  (api.createWeekendPlan as jest.Mock).mockRejectedValueOnce(new ApiError(503, 'unavailable', 'x'));
  render(wrap(<NewWeekendPlan />));
  await pickEnds();

  fireEvent.press(screen.getByTestId('weekend-submit'));
  await waitFor(() => expect(api.createWeekendPlan).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(screen.getByTestId('weekend-submit')).toBeEnabled());
  fireEvent.press(screen.getByTestId('weekend-submit'));
  await waitFor(() => expect(api.createWeekendPlan).toHaveBeenCalledTimes(2));

  const calls = (api.createWeekendPlan as jest.Mock).mock.calls;
  expect(calls[0][0].client_key).toBeTruthy();
  expect(calls[0][0].client_key).toBe(calls[1][0].client_key);
});

test('a save sent again after an edit is a different plan, so it carries a new key', async () => {
  (api.createWeekendPlan as jest.Mock).mockRejectedValueOnce(new ApiError(503, 'unavailable', 'x'));
  render(wrap(<NewWeekendPlan />));
  await pickEnds();

  fireEvent.press(screen.getByTestId('weekend-submit'));
  await waitFor(() => expect(api.createWeekendPlan).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(screen.getByTestId('weekend-submit')).toBeEnabled());

  // The first save may have gone through with its response lost: the edited
  // plan must not be answered with that one.
  fireEvent.press(screen.getByTestId('weekend-out-day-6'));
  fireEvent.press(screen.getByTestId('weekend-submit'));
  await waitFor(() => expect(api.createWeekendPlan).toHaveBeenCalledTimes(2));

  const calls = (api.createWeekendPlan as jest.Mock).mock.calls;
  expect(calls[1][0].depart_dow).toBe(6);
  expect(calls[1][0].client_key).toBeTruthy();
  expect(calls[1][0].client_key).not.toBe(calls[0][0].client_key);
});

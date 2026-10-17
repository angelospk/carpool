import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { SeatOffers } from '../SeatOffers';
import { ApiError } from '../../api/client';
import { api, type DriverOffer } from '../../api/endpoints';

const mockPush = jest.fn();
jest.mock('expo-router', () => ({ useRouter: () => ({ push: mockPush }) }));
jest.mock('../../api/endpoints', () => ({
  api: { offers: jest.fn(), acceptOffer: jest.fn(), declineOffer: jest.fn() },
}));
jest.mock('../../state/session', () => ({
  useSession: (select: (s: unknown) => unknown) => select({ status: 'signedIn' }),
}));

let client: QueryClient;
function wrap(node: ReactNode) {
  client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const offer = (over: Partial<DriverOffer> = {}): DriverOffer => ({
  id: 'ask-1',
  request_id: 'req-1',
  occurrence_id: 'occ-1',
  driver_name: 'Γιώργος',
  driver_photo_url: null,
  driver_rating: 4.5,
  driver_rating_count: 12,
  car: { make: 'Toyota', model: 'Yaris', colour: 'λευκό' },
  depart_earliest: '2026-10-16T12:00:00Z', // Friday 15:00 in Athens
  price_eur: 8,
  seats: 1,
  detour_min: 4,
  detour_km: 1.5,
  origin_label: 'Αλεξανδρούπολη',
  destination_label: 'Θεσσαλονίκη',
  expires_at: '2026-10-16T09:00:00Z',
  ...over,
});

beforeEach(() => {
  jest.clearAllMocks();
  (api.offers as jest.Mock).mockResolvedValue({ offers: [offer()] });
});

test('no offers draws nothing', async () => {
  (api.offers as jest.Mock).mockResolvedValue({ offers: [] });
  render(wrap(<SeatOffers />));

  await new Promise((r) => setTimeout(r, 0));
  expect(screen.queryByTestId('seat-offers')).toBeNull();
});

test('an offer says who drives, in what, when, for how much', async () => {
  render(wrap(<SeatOffers />));

  expect(await screen.findByText('Γιώργος')).toBeTruthy();
  expect(screen.getByTestId('seat-offer-rating-ask-1')).toHaveTextContent('★ 4,5 (12)');
  expect(screen.getByText('Toyota · Yaris · λευκό')).toBeTruthy();
  expect(screen.getByText(/15:00/)).toBeTruthy();
  expect(screen.getByTestId('seat-offer-price-ask-1')).toHaveTextContent(/^8,00/);
});

test('several seats show the price of each and of all', async () => {
  (api.offers as jest.Mock).mockResolvedValue({ offers: [offer({ seats: 2 })] });
  render(wrap(<SeatOffers />));

  expect(await screen.findByTestId('seat-offer-price-ask-1')).toHaveTextContent(/8,00.*ανά θέση.*σύνολο.*16,00/);
});

test('«Ναι» books the seat and shows the trip', async () => {
  (api.acceptOffer as jest.Mock).mockResolvedValue({ booking_id: 'bk-1' });
  render(wrap(<SeatOffers />));

  fireEvent.press(await screen.findByTestId('seat-offer-accept-ask-1'));

  await waitFor(() => expect(api.acceptOffer).toHaveBeenCalledWith('ask-1'));
  await waitFor(() => expect(mockPush).toHaveBeenCalledWith('/trip/occ-1'));
});

test('accepting refreshes the trips and the requests', async () => {
  (api.acceptOffer as jest.Mock).mockResolvedValue({ booking_id: 'bk-1' });
  render(wrap(<SeatOffers />));
  const spy = jest.spyOn(client, 'invalidateQueries');

  fireEvent.press(await screen.findByTestId('seat-offer-accept-ask-1'));

  await waitFor(() => expect(mockPush).toHaveBeenCalled());
  const keys = spy.mock.calls.map((c) => JSON.stringify((c[0] as { queryKey: unknown }).queryKey));
  expect(keys).toEqual(expect.arrayContaining(['["trips"]', '["requests"]', '["offers"]']));
});

test('«Όχι» declines it and stays on the home screen', async () => {
  (api.declineOffer as jest.Mock).mockResolvedValue(undefined);
  render(wrap(<SeatOffers />));

  fireEvent.press(await screen.findByTestId('seat-offer-decline-ask-1'));

  await waitFor(() => expect(api.declineOffer).toHaveBeenCalledWith('ask-1'));
  expect(mockPush).not.toHaveBeenCalled();
});

test('a seat that is gone is said in words, not as an error', async () => {
  (api.acceptOffer as jest.Mock).mockRejectedValue(new ApiError(409, 'no_seats', 'no seats'));
  render(wrap(<SeatOffers />));

  fireEvent.press(await screen.findByTestId('seat-offer-accept-ask-1'));

  expect(await screen.findByText('Δεν υπάρχουν ελεύθερες θέσεις')).toBeTruthy();
  expect(mockPush).not.toHaveBeenCalled();
});

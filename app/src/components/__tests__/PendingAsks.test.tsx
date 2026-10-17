import { render, screen, fireEvent } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { PendingAsks } from '../PendingAsks';
import { api } from '../../api/endpoints';

const mockPush = jest.fn();
jest.mock('expo-router', () => ({ useRouter: () => ({ push: mockPush }) }));
jest.mock('../../api/endpoints', () => ({ api: { pendingAsks: jest.fn() } }));
jest.mock('../../state/session', () => ({
  useSession: (select: (s: unknown) => unknown) => select({ status: 'signedIn' }),
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const ask = {
  id: 'ask-1',
  occurrence_id: 'occ-1',
  passenger_name: 'Ελένη',
  passenger_photo_url: null,
  passenger_rating: 4.5,
  passenger_rating_count: 2,
  shared_groups: ['Σχολή Α'],
  luggage: 'suitcase',
  luggage_count: 2,
  window_start: '2026-10-01T03:00:00Z',
  window_end: '2026-10-01T07:00:00Z',
  origin_label: 'Αλεξανδρούπολη',
  destination_label: 'Φέρες',
  detour_min: 6,
  detour_km: 2.1,
  price_eur: 3.5,
  seats: 1,
  depart_earliest: '2026-10-01T04:30:00Z',
  expires_at: '2026-10-01T04:00:00Z',
};

beforeEach(() => jest.clearAllMocks());

// Missing the push used to mean missing the passenger: nothing in the app
// listed a question waiting on a driver.
test('a waiting ask is on the home screen and opens', async () => {
  (api.pendingAsks as jest.Mock).mockResolvedValue({ asks: [ask] });
  render(wrap(<PendingAsks />));

  fireEvent.press(await screen.findByTestId('pending-ask-ask-1'));

  expect(screen.getByText(/Ελένη/)).toBeTruthy();
  expect(mockPush).toHaveBeenCalledWith('/ask/ask-1');
});

test('nothing waiting draws nothing', async () => {
  (api.pendingAsks as jest.Mock).mockResolvedValue({ asks: [] });
  render(wrap(<PendingAsks />));

  await new Promise((r) => setTimeout(r, 0));
  expect(screen.queryByTestId('pending-asks')).toBeNull();
});

// The driver decides on the card: who, how well rated, what they carry, and the
// price per seat with the total when there are several.
test('the card says who is asking, when, what they carry and the price', async () => {
  (api.pendingAsks as jest.Mock).mockResolvedValue({ asks: [{ ...ask, seats: 2 }] });
  render(wrap(<PendingAsks />));

  expect(await screen.findByTestId('ask-rating')).toHaveTextContent(/★ 4,5 \(2\)/);
  expect(screen.getByTestId('ask-shared-groups')).toBeTruthy();
  expect(screen.getByTestId('ask-window')).toHaveTextContent(/ζητά \d\d:\d\d–\d\d:\d\d/);
  expect(screen.getByTestId('ask-luggage')).toHaveTextContent(/2 × Κανονική βαλίτσα/);
  expect(screen.getByTestId('pending-ask-price-ask-1')).toHaveTextContent(/3,50.*ανά θέση.*σύνολο.*7,00/);
});

test('one seat is just the price, and a newcomer has no stars', async () => {
  (api.pendingAsks as jest.Mock).mockResolvedValue({
    asks: [{ ...ask, passenger_rating: null, passenger_rating_count: 0, luggage: 'none', luggage_count: 0 }],
  });
  render(wrap(<PendingAsks />));

  const price = await screen.findByTestId('pending-ask-price-ask-1');
  expect(price).toHaveTextContent(/3,50/);
  expect(price).not.toHaveTextContent(/ανά θέση/);
  expect(screen.getByTestId('ask-rating')).toHaveTextContent(/Χωρίς αξιολογήσεις/);
  expect(screen.queryByTestId('ask-luggage')).toBeNull();
});

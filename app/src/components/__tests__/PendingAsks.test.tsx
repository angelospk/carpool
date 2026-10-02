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

import { render, screen } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { PendingAsks } from '../PendingAsks';
import { api } from '../../api/endpoints';

jest.mock('expo-router', () => ({ useRouter: () => ({ push: jest.fn() }) }));
jest.mock('../../api/endpoints', () => ({ api: { pendingAsks: jest.fn() } }));
jest.mock('../../state/session', () => ({
  useSession: (select: (s: unknown) => unknown) => select({ status: 'signedIn' }),
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const ask = (id: string, occurrence: string) => ({
  id,
  occurrence_id: occurrence,
  passenger_name: 'Ελένη',
  origin_label: 'Αλεξανδρούπολη',
  destination_label: 'Φέρες',
  detour_min: 6,
  detour_km: 2.1,
  price_eur: 3.5,
  seats: 1,
  passenger_photo_url: null,
  passenger_rating: null,
  passenger_rating_count: 0,
  shared_groups: [],
  luggage: 'none',
  luggage_count: 0,
  window_start: '2026-10-01T04:00:00Z',
  window_end: '2026-10-01T05:00:00Z',
  depart_earliest: '2026-10-01T04:30:00Z',
  expires_at: '2026-10-01T04:00:00Z',
});

beforeEach(() => {
  jest.clearAllMocks();
  (api.pendingAsks as jest.Mock).mockResolvedValue({ asks: [ask('a1', 'occ-1'), ask('a2', 'occ-2')] });
});

// QA F8: on a trip's own screen, only the asks for that trip.
test('with an occurrence it lists only the asks for it', async () => {
  render(wrap(<PendingAsks occurrenceId="occ-1" />));

  expect(await screen.findByTestId('pending-ask-a1')).toBeTruthy();
  expect(screen.queryByTestId('pending-ask-a2')).toBeNull();
});

test('with an occurrence and no ask for it, it draws nothing', async () => {
  render(wrap(<PendingAsks occurrenceId="occ-9" />));

  await new Promise((r) => setTimeout(r, 0));
  expect(screen.queryByTestId('pending-asks')).toBeNull();
});

test('without one it lists every ask', async () => {
  render(wrap(<PendingAsks />));

  expect(await screen.findByTestId('pending-ask-a1')).toBeTruthy();
  expect(screen.getByTestId('pending-ask-a2')).toBeTruthy();
});

import { render, screen, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { LiveMapCard } from '../LiveMapCard';
import { api } from '../../api/endpoints';

jest.mock('../../api/endpoints', () => ({ api: { liveMap: jest.fn() } }));
jest.mock('../../map/Map', () => ({
  Map: ({ lines, testID }: { lines?: string[]; testID?: string }) => {
    const { Text } = require('react-native');
    return <Text testID={testID}>{`lines:${(lines ?? []).length}`}</Text>;
  },
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

afterEach(() => jest.clearAllMocks());

// What is on the road now: the line each car takes and where it goes, with
// nobody's name on it.
test('draws every car on the road and says where each goes', async () => {
  (api.liveMap as jest.Mock).mockResolvedValue({
    trips: [
      { from: 'Σουφλί', to: 'Αλεξανδρούπολη', seats_free: 2, polyline: '_c`|@_c`|@' },
      { from: 'Φέρες', to: 'Ορεστιάδα', seats_free: 0, polyline: '_c`|@_c`|@' },
    ],
  });
  render(wrap(<LiveMapCard />));

  expect(await screen.findByText('lines:2')).toBeTruthy();
  expect(screen.getByText(/Σουφλί → Αλεξανδρούπολη/)).toBeTruthy();
  expect(screen.getByText(/Φέρες → Ορεστιάδα/)).toBeTruthy();
});

// An empty card at the top of the search screen only pushed the form down:
// with nobody on the road there is nothing to decide by, so it is not drawn.
test('draws nothing when nobody is on the road', async () => {
  (api.liveMap as jest.Mock).mockResolvedValue({ trips: [] });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <LiveMapCard />
    </QueryClientProvider>,
  );

  // Answered, so a card would be on screen by now if one were coming.
  await waitFor(() => expect(client.getQueryState(['live-map'])?.status).toBe('success'));
  expect(screen.queryByTestId('live-map')).toBeNull();
  expect(screen.queryByTestId('live-map-map')).toBeNull();
});

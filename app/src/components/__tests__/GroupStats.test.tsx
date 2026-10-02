import { render, screen } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { GroupStats } from '../GroupStats';
import { api } from '../../api/endpoints';

jest.mock('../../api/endpoints', () => ({ api: { groupStats: jest.fn() } }));

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

// What an admin takes into a meeting: this month, in four numbers, without
// opening a spreadsheet.
test('an admin sees the month counted', async () => {
  (api.groupStats as jest.Mock).mockResolvedValue({ members: 40, drivers: 6, trips: 23, seats: 27 });
  render(wrap(<GroupStats groupId="g-1" />));

  expect(await screen.findByTestId('group-stats-trips')).toHaveTextContent('23');
  expect(screen.getByTestId('group-stats-members')).toHaveTextContent('40');
  expect(screen.getByTestId('group-stats-drivers')).toHaveTextContent('6');
  expect(api.groupStats).toHaveBeenCalledWith('g-1');
});

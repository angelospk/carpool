import { render, screen } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { Supply } from '../Supply';
import { api } from '../../api/endpoints';

jest.mock('../../api/endpoints', () => ({ api: { supply: jest.fn() } }));

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const pin = (name: string, lon: number, lat: number) => ({
  id: `node/${name}`,
  area: '',
  kind: 'town' as const,
  name,
  label: name,
  lon,
  lat,
});
const from = pin('Αλεξανδρούπολη', 25.87, 40.84);
const to = pin('Φέρες', 26.17, 40.89);

beforeEach(() => jest.clearAllMocks());

// An empty app looked the same whether a road had four cars on it or none.
test('says that cars go that way that day, not how many', async () => {
  (api.supply as jest.Mock).mockResolvedValue({ status: 'several' });
  render(wrap(<Supply from={from} to={to} date="2026-10-01" />));

  expect(await screen.findByTestId('supply')).toHaveTextContent(/Τουλάχιστον τρεις οδηγοί/);
  expect(api.supply).toHaveBeenCalledWith({
    origin_lon: 25.87,
    origin_lat: 40.84,
    destination_lon: 26.17,
    destination_lat: 40.89,
    date: '2026-10-01',
  });
});

// Below the floor the server says nothing, and neither does the screen. It is
// not "nobody": asking still works, and a request waits for a route.
test('says nothing when the server will not say', async () => {
  (api.supply as jest.Mock).mockResolvedValue({ status: 'unknown' });
  render(wrap(<Supply from={from} to={to} date="2026-10-01" />));

  await new Promise((r) => setTimeout(r, 0));
  expect(screen.queryByTestId('supply')).toBeNull();
});

test('asks nothing until both ends are set', () => {
  render(wrap(<Supply from={from} to={null} date="2026-10-01" />));
  expect(api.supply).not.toHaveBeenCalled();
});

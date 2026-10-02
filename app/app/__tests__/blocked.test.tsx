import { render as rtlRender, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactElement } from 'react';

import BlockedScreen from '../profile/blocked';
import { api } from '../../src/api/endpoints';

jest.mock('../../src/api/endpoints', () => ({
  api: { myBlocks: jest.fn(), unblockUser: jest.fn() },
}));

function render(node: ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return rtlRender(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
}

beforeEach(() => {
  jest.clearAllMocks();
  (api.unblockUser as jest.Mock).mockResolvedValue(undefined);
});

test('lists who you blocked and lets you undo it', async () => {
  (api.myBlocks as jest.Mock)
    .mockResolvedValueOnce({ blocked: [{ id: 'u-1', name: 'Γιάννης', photo_url: null }] })
    .mockResolvedValue({ blocked: [] });
  render(<BlockedScreen />);

  expect(await screen.findByText('Γιάννης')).toBeTruthy();
  fireEvent.press(screen.getByTestId('unblock-u-1'));

  await waitFor(() => expect(api.unblockUser).toHaveBeenCalledWith('u-1'));
  expect(await screen.findByTestId('blocked-empty')).toBeTruthy();
});

test('nobody blocked says so', async () => {
  (api.myBlocks as jest.Mock).mockResolvedValue({ blocked: [] });
  render(<BlockedScreen />);

  expect(await screen.findByTestId('blocked-empty')).toBeTruthy();
});

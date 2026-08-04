import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import PublishScreen from '../(tabs)/publish';
import { api } from '../../src/api/endpoints';

jest.mock('../../src/api/endpoints', () => ({
  api: { publishRoute: jest.fn(), places: jest.fn() },
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const pin = (label: string, lon: number, lat: number) => ({
  id: `node/${label}`,
  area: 'Δοκιμής',
  kind: 'town' as const,
  name: label,
  lon,
  lat,
});

// Fills in enough of the form that the publish button becomes usable.
async function fillRoute() {
  (api.places as jest.Mock).mockResolvedValue({ places: [pin('Αλεξανδρούπολη', 25.87, 40.84)] });
  render(wrap(<PublishScreen />));

  for (const field of ['publish-origin', 'publish-destination']) {
    fireEvent.changeText(screen.getByTestId(`${field}-input`), 'αλεξ');
    await waitFor(() => screen.getByTestId(`${field}-node/Αλεξανδρούπολη`));
    fireEvent.press(screen.getByTestId(`${field}-node/Αλεξανδρούπολη`));
  }
}

beforeEach(() => {
  jest.clearAllMocks();
  (api.publishRoute as jest.Mock).mockResolvedValue({
    template_id: 't1',
    route_km: 17,
    ends_at_port: false,
  });
});

// The default is what the driver already told us once, on their profile. Asking
// again on every trip would be a form nobody finishes.
test('publishing without touching the detour sends nothing about it', async () => {
  await fillRoute();
  fireEvent.press(screen.getByTestId('publish-submit'));

  await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());
  const body = (api.publishRoute as jest.Mock).mock.calls[0][0];
  expect(body.detour_km).toBeUndefined();
  expect(body.allow_stops).toBeUndefined();
});

// The whole point: this trip, not this driver. Somebody doing the long run to
// Thessaloniki has time to go out of their way; the same person on Monday's
// commute has none.
test('a detour set on the form is sent with the trip', async () => {
  await fillRoute();

  fireEvent.press(screen.getByTestId('detour-km-plus'));
  fireEvent.press(screen.getByTestId('detour-km-plus'));
  fireEvent.press(screen.getByTestId('publish-submit'));

  await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());
  const body = (api.publishRoute as jest.Mock).mock.calls[0][0];
  expect(body.allow_stops).toBe(true);
  expect(body.detour_km).toBeGreaterThan(0);
});

// Refusing stops has to be sayable, and it is not the same as a budget of zero:
// pulling over costs time even where the map says the detour is nothing.
test('a driver can say no intermediate stops at all', async () => {
  await fillRoute();

  fireEvent.press(screen.getByTestId('detour-allow'));
  fireEvent.press(screen.getByTestId('publish-submit'));

  await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());
  const body = (api.publishRoute as jest.Mock).mock.calls[0][0];
  expect(body.allow_stops).toBe(false);
});

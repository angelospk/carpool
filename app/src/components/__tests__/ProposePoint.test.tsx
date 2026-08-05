import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { ProposePoint } from '../ProposePoint';
import { api } from '../../api/endpoints';
import { ApiError } from '../../api/client';

jest.mock('../../api/endpoints', () => ({
  api: { proposeMeetingPoint: jest.fn() },
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const feres = { lon: 26.174, lat: 40.894, label: 'Φέρες', id: 'node/139260653' };

const open = () => {
  render(wrap(<ProposePoint place={feres} testID="pp" />));
  fireEvent.press(screen.getByTestId('pp-open'));
};

beforeEach(() => {
  jest.clearAllMocks();
  (api.proposeMeetingPoint as jest.Mock).mockResolvedValue({
    id: 'r1',
    place_id: feres.id,
    place_name: 'Φέρες',
    status: 'pending',
    created_at: '2026-08-05T10:00:00Z',
  });
});

// The form is not in the way until somebody wants it. Most people choosing a
// pickup point are not correcting the map.
test('the form is behind an invitation rather than always open', () => {
  render(wrap(<ProposePoint place={feres} testID="pp" />));
  expect(screen.queryByTestId('pp-name')).toBeNull();
  expect(screen.getByTestId('pp-open')).toBeTruthy();
});

test('a suggestion carries the place, the name and the kind', async () => {
  open();
  fireEvent.changeText(screen.getByTestId('pp-name'), 'ΚΤΕΛ Φερών');
  fireEvent.press(screen.getByTestId('pp-kind-bus_station'));
  fireEvent.press(screen.getByTestId('pp-submit'));

  await waitFor(() => expect(api.proposeMeetingPoint).toHaveBeenCalled());
  const body = (api.proposeMeetingPoint as jest.Mock).mock.calls[0][0];
  expect(body.place_id).toBe(feres.id);
  expect(body.name).toBe('ΚΤΕΛ Φερών');
  expect(body.kind).toBe('bus_station');
});

// The one thing that must not be misunderstood: this is not a meeting point
// yet. Telling somebody it is would send a stranger to stand somewhere nobody
// has checked.
test('what comes back says it is waiting, not that it is done', async () => {
  open();
  fireEvent.changeText(screen.getByTestId('pp-name'), 'ΚΤΕΛ Φερών');
  fireEvent.press(screen.getByTestId('pp-submit'));

  expect(await screen.findByTestId('pp-sent')).toBeTruthy();
  // And it is not offered as something to pick.
  expect(screen.queryByTestId('pp-submit')).toBeNull();
});

// A retry after a timeout must not become a second suggestion, so the key is
// made once rather than on every render.
test('a retry carries the same idempotency key', async () => {
  (api.proposeMeetingPoint as jest.Mock).mockRejectedValueOnce(new Error('timeout'));
  open();
  fireEvent.changeText(screen.getByTestId('pp-name'), 'ΚΤΕΛ Φερών');

  fireEvent.press(screen.getByTestId('pp-submit'));
  await waitFor(() => expect(api.proposeMeetingPoint).toHaveBeenCalledTimes(1));
  fireEvent.press(screen.getByTestId('pp-submit'));
  await waitFor(() => expect(api.proposeMeetingPoint).toHaveBeenCalledTimes(2));

  const calls = (api.proposeMeetingPoint as jest.Mock).mock.calls;
  expect(calls[0][0].idempotency_key).toBe(calls[1][0].idempotency_key);
  expect(calls[0][0].idempotency_key).toBeTruthy();
});

test('an empty or one-letter name cannot be sent', () => {
  open();
  expect(screen.getByTestId('pp-submit').props.accessibilityState?.disabled).toBe(true);

  fireEvent.changeText(screen.getByTestId('pp-name'), 'Κ');
  expect(screen.getByTestId('pp-submit').props.accessibilityState?.disabled).toBe(true);

  fireEvent.changeText(screen.getByTestId('pp-name'), 'ΚΤΕΛ');
  expect(screen.getByTestId('pp-submit').props.accessibilityState?.disabled).toBe(false);
});

// Each refusal the server gives is something the person can act on, so each one
// is said rather than collapsed into "something went wrong".
test('a refusal is reported as the reason it was refused', async () => {
  const cases: Array<[string, RegExp]> = [
    ['too_many', /σήμερα/i],
    ['duplicate_point', /ήδη/i],
    ['too_far', /περιοχή/i],
  ];

  for (const [code, expected] of cases) {
    jest.clearAllMocks();
    (api.proposeMeetingPoint as jest.Mock).mockRejectedValue(new ApiError(400, code, 'no'));

    open();
    fireEvent.changeText(screen.getByTestId('pp-name'), 'Κάπου');
    fireEvent.press(screen.getByTestId('pp-submit'));

    expect(await screen.findByText(expected)).toBeTruthy();
    screen.unmount();
  }
});

// Without a map, a suggestion is a name and the settlement's own coordinate,
// and a moderator has to work out where it meant.
describe('with a map configured', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_MAP_TILES_URL = 'https://tiles.example/{z}/{x}/{y}.png';
  });
  afterEach(() => {
    delete process.env.EXPO_PUBLIC_MAP_TILES_URL;
  });

  it('offers the map to say where exactly', () => {
    open();
    expect(screen.getByTestId('pp-map')).toBeTruthy();
  });

  // The point of the map. Without it a suggestion is a name and the middle of
  // the village, and a moderator has to work out where it meant.
  it('sends where the pin was dropped, not the middle of the village', async () => {
    const { STUB_TAP } = jest.requireMock('react-native-maps');

    open();
    fireEvent.changeText(screen.getByTestId('pp-name'), 'ΚΤΕΛ Φερών');
    fireEvent.press(screen.getByTestId('stub-map-canvas'));
    fireEvent.press(screen.getByTestId('pp-submit'));

    await waitFor(() => expect(api.proposeMeetingPoint).toHaveBeenCalled());
    const body = (api.proposeMeetingPoint as jest.Mock).mock.calls[0][0];
    expect(body.lon).toBe(STUB_TAP.longitude);
    expect(body.lat).toBe(STUB_TAP.latitude);
    expect(body.lon).not.toBe(feres.lon);
  });
});

it('sends the settlement coordinate when there is no map to drop a pin on', async () => {
  delete process.env.EXPO_PUBLIC_MAP_TILES_URL;

  open();
  expect(screen.queryByTestId('pp-map')).toBeNull();

  fireEvent.changeText(screen.getByTestId('pp-name'), 'ΚΤΕΛ Φερών');
  fireEvent.press(screen.getByTestId('pp-submit'));

  await waitFor(() => expect(api.proposeMeetingPoint).toHaveBeenCalled());
  const body = (api.proposeMeetingPoint as jest.Mock).mock.calls[0][0];
  expect(body.lon).toBe(feres.lon);
  expect(body.lat).toBe(feres.lat);
});

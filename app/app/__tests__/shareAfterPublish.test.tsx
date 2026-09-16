import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { Share } from 'react-native';

import PublishScreen from '../(tabs)/publish';
import { api } from '../../src/api/endpoints';

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { publishRoute: jest.fn(), places: jest.fn(), meetingPoints: jest.fn(), vehicles: jest.fn() },
  LUGGAGE_SIZES: ['none', 'cabin', 'suitcase', 'bulky'],
}));

const car = {
  id: 'v1',
  name: 'Toyota Yaris',
  label: '',
  make: 'Toyota',
  model: 'Yaris',
  fuel: 'petrol',
  colour: 'λευκό',
  plate: 'ΑΒΓ-1234',
  consumption_l_100km: 5.6,
  consumption_source: 'dataset' as const,
  consumption_capped: false,
  is_default: true,
};

const pin = (label: string, lon: number, lat: number) => ({
  id: `node/${label}`,
  area: 'Δοκιμής',
  kind: 'town' as const,
  name: label,
  lon,
  lat,
});

function wrap(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

let share: jest.SpyInstance;

beforeEach(() => {
  jest.clearAllMocks();
  share = jest.spyOn(Share, 'share').mockResolvedValue({ action: 'sharedAction' });
  (api.vehicles as jest.Mock).mockResolvedValue([car]);
  (api.places as jest.Mock).mockResolvedValue({
    places: [pin('Αλεξανδρούπολη', 25.87, 40.84), pin('Σουφλί', 26.29, 41.19)],
  });
  (api.meetingPoints as jest.Mock).mockResolvedValue({ points: [] });
  (api.publishRoute as jest.Mock).mockResolvedValue({
    template_id: 'r1',
    route_km: 62.4,
    ends_at_port: false,
  });
});

afterEach(() => {
  share.mockRestore();
});

async function pickBothEnds() {
  render(wrap(<PublishScreen />));

  fireEvent.changeText(screen.getByTestId('publish-origin-input'), 'αλεξ');
  await waitFor(() => screen.getByTestId('publish-origin-node/Αλεξανδρούπολη'));
  fireEvent.press(screen.getByTestId('publish-origin-node/Αλεξανδρούπολη'));

  fireEvent.changeText(screen.getByTestId('publish-destination-input'), 'σουφ');
  await waitFor(() => screen.getByTestId('publish-destination-node/Σουφλί'));
  fireEvent.press(screen.getByTestId('publish-destination-node/Σουφλί'));
}

// Publishing and then being told the kilometres is the end of the road today.
// The moment the route exists is the moment the driver is most likely to tell
// somebody about it, so the way out is offered there.
test('a route just published can be shared from the confirmation', async () => {
  await pickBothEnds();

  fireEvent.press(screen.getByTestId('publish-submit'));
  await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());

  fireEvent.press(await screen.findByTestId('publish-share-route'));

  await waitFor(() => expect(share).toHaveBeenCalledTimes(1));
  const message = share.mock.calls[0][0].message as string;
  expect(message).toContain('Αλεξανδρούπολη → Σουφλί');
  expect(message).toContain('καθημερινές 07:30');
  expect(message).toMatch(/3 θέσεις/);
});

// The form is still on screen after publishing and still editable. What is
// shared is the route that exists, not whatever the fields happen to say when
// the button is pressed — otherwise the driver sends a schedule nobody can be
// picked up on.
test('the shared message describes what was published, not the form afterwards', async () => {
  await pickBothEnds();

  fireEvent.press(screen.getByTestId('publish-submit'));
  await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());

  // Two edits after the fact: a different schedule and a different time.
  fireEvent.press(screen.getByTestId('preset-presetWeekend'));

  fireEvent.press(await screen.findByTestId('publish-share-route'));

  await waitFor(() => expect(share).toHaveBeenCalledTimes(1));
  const message = share.mock.calls[0][0].message as string;
  expect(message).toContain('καθημερινές');
  expect(message).not.toContain('Σαββατοκύριακα');
});

// Publishing twice must not leave the first route's message behind the second
// route's button.
test('a second publish replaces what is offered for sharing', async () => {
  await pickBothEnds();

  fireEvent.press(screen.getByTestId('publish-submit'));
  await waitFor(() => expect(api.publishRoute).toHaveBeenCalledTimes(1));

  fireEvent.press(screen.getByTestId('preset-presetWeekend'));
  fireEvent.press(screen.getByTestId('publish-submit'));
  await waitFor(() => expect(api.publishRoute).toHaveBeenCalledTimes(2));

  fireEvent.press(await screen.findByTestId('publish-share-route'));

  await waitFor(() => expect(share).toHaveBeenCalledTimes(1));
  const message = share.mock.calls[0][0].message as string;
  expect(message).toContain('Σαββατοκύριακα');
  expect(message).not.toContain('καθημερινές');
});

// Nothing exists yet, so there is nothing to tell anybody about.
test('an unpublished form offers no share', async () => {
  await pickBothEnds();

  expect(screen.queryByTestId('publish-share-route')).toBeNull();
});

// A publish that failed leaves nothing published. Offering to share it would
// be offering a route that does not exist.
test('a failed publish offers no share', async () => {
  (api.publishRoute as jest.Mock).mockRejectedValue(new Error('nope'));

  await pickBothEnds();

  fireEvent.press(screen.getByTestId('publish-submit'));
  await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());

  expect(screen.queryByTestId('publish-share-route')).toBeNull();
});

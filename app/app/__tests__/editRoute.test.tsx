import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import PublishScreen from '../(tabs)/publish';
import { api } from '../../src/api/endpoints';

// «Αλλαγή» on a route opens this form filled in with it. Publishing puts the
// changed route out and retires the old one, which calls off its empty trips.
let mockParams: Record<string, string> = {};
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
  useFocusEffect: (effect: () => void) => require('react').useEffect(effect, []),
  useLocalSearchParams: () => mockParams,
}));

jest.mock('../../src/api/endpoints', () => ({
  api: {
    publishRoute: jest.fn(),
    retireRoute: jest.fn(),
    places: jest.fn(),
    placeNear: jest.fn(),
    meetingPoints: jest.fn(),
    vehicles: jest.fn(),
    myGroups: jest.fn(),
    shareRouteLink: jest.fn(),
    demand: jest.fn(),
  },
  LUGGAGE_SIZES: ['none', 'cabin', 'suitcase', 'bulky'],
}));

const car = {
  id: 'v1', name: 'Toyota Yaris', label: '', make: 'Toyota', model: 'Yaris', fuel: 'petrol',
  colour: 'λευκό', plate: 'ΑΒΓ-1234', consumption_l_100km: 5.6, consumption_source: 'dataset' as const,
  consumption_capped: false, is_default: true,
};

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false }, queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

beforeEach(() => {
  mockParams = {
    replaces: 'old-route',
    from_lon: '26.3', from_lat: '41.19', from_label: 'Σουφλί',
    to_lon: '25.87', to_lat: '40.84', to_label: 'Αλεξανδρούπολη',
    kind: 'recurring', weekdays: '6,7', earliest: '09:15', latest: '09:45', seats: '2',
  };
  (api.vehicles as jest.Mock).mockResolvedValue([car]);
  (api.myGroups as jest.Mock).mockResolvedValue({ groups: [] });
  (api.meetingPoints as jest.Mock).mockResolvedValue({ points: [] });
  (api.places as jest.Mock).mockResolvedValue({ places: [] });
  (api.demand as jest.Mock)?.mockResolvedValue?.({ roads: [] });
  (api.shareRouteLink as jest.Mock).mockRejectedValue(new Error('offline'));
  (api.publishRoute as jest.Mock).mockResolvedValue({ template_id: 'new-route', route_km: 62.4, ends_at_port: false });
  (api.retireRoute as jest.Mock).mockResolvedValue(undefined);
});
afterEach(() => jest.clearAllMocks());

test('the form opens filled in with the route being changed', async () => {
  render(wrap(<PublishScreen />));

  await waitFor(() => expect(screen.getByTestId('publish-origin-input').props.value).toBe('Σουφλί'));
  expect(screen.getByTestId('publish-destination-input').props.value).toBe('Αλεξανδρούπολη');
  expect(screen.getByTestId('depart-earliest').props.value).toBe('09:15');
  expect(screen.getByTestId('editing-route')).toBeTruthy();
});

test('publishing the change retires the old route', async () => {
  render(wrap(<PublishScreen />));
  await waitFor(() => expect(screen.getByTestId('publish-origin-input').props.value).toBe('Σουφλί'));

  await waitFor(() => expect(screen.getByTestId('publish-submit').props.accessibilityState?.disabled).toBeFalsy());
  fireEvent.press(screen.getByTestId('publish-submit'));

  await waitFor(() => expect(api.retireRoute).toHaveBeenCalledWith('old-route'));
  const sent = (api.publishRoute as jest.Mock).mock.calls[0][0];
  expect(sent).toEqual(expect.objectContaining({ weekdays: [6, 7], depart_earliest: '09:15', seats: 2 }));
});

test('a failed publish leaves the old route alone', async () => {
  (api.publishRoute as jest.Mock).mockRejectedValue(new Error('nope'));
  render(wrap(<PublishScreen />));
  await waitFor(() => expect(screen.getByTestId('publish-origin-input').props.value).toBe('Σουφλί'));

  await waitFor(() => expect(screen.getByTestId('publish-submit').props.accessibilityState?.disabled).toBeFalsy());
  fireEvent.press(screen.getByTestId('publish-submit'));

  await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());
  expect(api.retireRoute).not.toHaveBeenCalled();
});

// «Φεύγω τώρα» is a single trip, not a new version of the weekly route: it
// must not retire the route being changed.
test('leaving now while changing a route does not retire it', async () => {
  render(wrap(<PublishScreen />));
  await waitFor(() => expect(screen.getByTestId('publish-origin-input').props.value).toBe('Σουφλί'));

  fireEvent.press(screen.getByTestId('schedule-now'));
  expect(screen.queryByTestId('editing-route')).toBeNull();
  await waitFor(() => expect(screen.getByTestId('publish-submit').props.accessibilityState?.disabled).toBeFalsy());
  fireEvent.press(screen.getByTestId('publish-submit'));

  await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());
  expect(api.retireRoute).not.toHaveBeenCalled();
});

// Review found this: a group-only route came back as one offered to everybody,
// because the group was not carried and the form's default is "everybody".
test('a group-only route stays group-only, with every other choice kept', async () => {
  mockParams = {
    ...mockParams,
    group: 'g-hospital',
    vehicle: 'v1',
    pricing: 'formula',
    share: '0.5',
    luggage: 'cabin',
    luggage_capacity: '2',
    allow_stops: 'false',
    detour_km: '7',
  };
  (api.myGroups as jest.Mock).mockResolvedValue({ groups: [{ id: 'g-hospital', name: 'Νοσοκομείο' }] });
  render(wrap(<PublishScreen />));
  await waitFor(() => expect(screen.getByTestId('publish-origin-input').props.value).toBe('Σουφλί'));

  await waitFor(() => expect(screen.getByTestId('publish-submit').props.accessibilityState?.disabled).toBeFalsy());
  fireEvent.press(screen.getByTestId('publish-submit'));

  await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());
  expect((api.publishRoute as jest.Mock).mock.calls[0][0]).toEqual(
    expect.objectContaining({
      group_id: 'g-hospital',
      vehicle_id: 'v1',
      contribution_share: 0.5,
      luggage: 'cabin',
      luggage_capacity: 2,
      allow_stops: false,
      detour_km: 7,
    }),
  );
});

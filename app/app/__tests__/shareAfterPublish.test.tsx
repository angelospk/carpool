import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { Share } from 'react-native';

import PublishScreen from '../(tabs)/publish';
import { ApiError } from '../../src/api/client';
import el from '../../src/i18n/el.json';
import { api } from '../../src/api/endpoints';

const mockPush = jest.fn();
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush, replace: jest.fn() }),
  // Focus is once, on mount, which is what a test screen gets.
  useFocusEffect: (effect: () => void) => require('react').useEffect(effect, []),
  useLocalSearchParams: () => ({}),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: {
    publishRoute: jest.fn(),
    places: jest.fn(),
    meetingPoints: jest.fn(),
    vehicles: jest.fn(),
    myGroups: jest.fn(),
    shareRouteLink: jest.fn(),
  },
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
  (api.myGroups as jest.Mock).mockResolvedValue({ groups: [] });
  (api.shareRouteLink as jest.Mock).mockRejectedValue(new Error('offline'));
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
  // These tests are about a weekly route's message; the form starts one-off.
  fireEvent.press(screen.getByTestId('schedule-recurring'));
  await pickEnds();
}

async function pickEnds() {
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

  // Two edits after the fact, so a regression that read either field live is
  // caught: a different schedule and a different departure time.
  fireEvent.press(screen.getByTestId('preset-presetWeekend'));
  fireEvent.changeText(screen.getByTestId('depart-earliest'), '18:45');

  fireEvent.press(await screen.findByTestId('publish-share-route'));

  await waitFor(() => expect(share).toHaveBeenCalledTimes(1));
  const message = share.mock.calls[0][0].message as string;
  expect(message).toContain('καθημερινές');
  expect(message).not.toContain('Σαββατοκύριακα');
  expect(message).toContain('07:30');
  expect(message).not.toContain('18:45');
});

// Publishing twice must not leave the first route's message behind the second
// route's button.
test('a second publish replaces what is offered for sharing', async () => {
  await pickBothEnds();

  // Waits for the first publish to have *settled*, not just to have been
  // called: the submit button is disabled while the mutation is in flight, so
  // pressing again too early is a press that does nothing and a test that
  // passes on timing.
  fireEvent.press(screen.getByTestId('publish-submit'));
  await waitFor(() => expect(screen.getByTestId('publish-share-route')).toBeTruthy());

  await pickEnds();
  fireEvent.press(screen.getByTestId('preset-presetWeekend'));
  fireEvent.press(screen.getByTestId('publish-submit'));
  await waitFor(() => expect(api.publishRoute).toHaveBeenCalledTimes(2));

  fireEvent.press(await screen.findByTestId('publish-share-route'));

  await waitFor(() => expect(share).toHaveBeenCalledTimes(1));
  const message = share.mock.calls[0][0].message as string;
  expect(message).toContain('Σαββατοκύριακα');
  expect(message).not.toContain('καθημερινές');
});

// A published form used to stay filled in, so a second press published the
// same route again, and starting the next route meant clearing the old one by
// hand. The places go; the schedule, seats and car stay, because a driver's
// next route usually shares them.
test('a route just published empties the places, so it cannot go out twice', async () => {
  await pickBothEnds();

  fireEvent.press(screen.getByTestId('publish-submit'));
  await waitFor(() => expect(screen.getByTestId('publish-share-route')).toBeTruthy());

  expect(screen.getByTestId('publish-origin-input').props.value).toBe('');
  expect(screen.getByTestId('publish-destination-input').props.value).toBe('');
  fireEvent.press(screen.getByTestId('publish-submit'));
  expect(api.publishRoute).toHaveBeenCalledTimes(1);
});

// A failure is the opposite case: the driver fixes one thing and tries again.
test('a failed publish keeps the places for another try', async () => {
  (api.publishRoute as jest.Mock).mockRejectedValue(new Error('nope'));
  await pickBothEnds();

  fireEvent.press(screen.getByTestId('publish-submit'));
  await waitFor(() => expect(screen.getByText(el.common.error)).toBeTruthy());

  expect(screen.getByTestId('publish-origin-input').props.value).not.toBe('');
});

// Nothing exists yet, so there is nothing to tell anybody about.
test('an unpublished form offers no share', async () => {
  await pickBothEnds();

  expect(screen.queryByTestId('publish-share-route')).toBeNull();
});

// A publish that failed leaves nothing published. Offering to share it would
// be offering a route that does not exist.
//
// Waits for the failure to be *reported* rather than merely for the request to
// have gone out: an earlier version asserted straight after the call and so
// passed before any error handling ran, which means it would have kept passing
// if that handling stopped working.
test('a failed publish offers no share', async () => {
  (api.publishRoute as jest.Mock).mockRejectedValue(new Error('nope'));

  await pickBothEnds();

  fireEvent.press(screen.getByTestId('publish-submit'));

  await waitFor(() => expect(screen.getByText(el.common.error)).toBeTruthy());
  expect(screen.queryByTestId('publish-share-route')).toBeNull();
});

// The route published a minute ago still exists when a second attempt fails, so
// clearing the share on error was wrong twice over: it withdrew a truthful
// offer, and it left the button next to an error message when it did not.
// Clearing happens when a new attempt *starts* instead, which is the only
// moment at which "what you just published" stops being the earlier route.
test('a failed second publish does not leave a stale share behind an error', async () => {
  await pickBothEnds();

  fireEvent.press(screen.getByTestId('publish-submit'));
  await waitFor(() => expect(screen.getByTestId('publish-share-route')).toBeTruthy());

  (api.publishRoute as jest.Mock).mockRejectedValue(new Error('nope'));
  await pickEnds();
  fireEvent.press(screen.getByTestId('preset-presetWeekend'));
  fireEvent.press(screen.getByTestId('publish-submit'));

  await waitFor(() => expect(screen.getByText(el.common.error)).toBeTruthy());
  // No share at all, rather than one describing the first route under a
  // sentence saying the second one failed.
  expect(screen.queryByTestId('publish-share-route')).toBeNull();
});

// A place the routing extract does not cover comes back as its own code, and
// the driver needs to be told that rather than "something went wrong". Without
// the snapping bound in geo.SnapRadiusMetres this was not even an error: OSRM
// snapped the far end hundreds of kilometres and answered with a route.
test('a place outside the routed area says so in its own words', async () => {
  (api.publishRoute as jest.Mock).mockRejectedValue(
    new ApiError(400, 'outside_service_area', 'one of those places is outside the area'),
  );

  await pickBothEnds();

  fireEvent.press(screen.getByTestId('publish-submit'));

  await waitFor(() => expect(screen.getByText(el.publish.outsideServiceArea)).toBeTruthy());
  expect(screen.queryByTestId('publish-share-route')).toBeNull();
});

// Review found this: while the groups were loading, or if they failed, the
// "who sees it" choice was not on screen and publishing went out to everybody.
// A driver who meant "only the hospital" never saw the question.
test('publishing waits until the groups have loaded, and stays shut if they failed', async () => {
  (api.myGroups as jest.Mock).mockRejectedValue(new Error('offline'));
  await pickBothEnds();

  await waitFor(() => expect(screen.getByTestId('audience-retry')).toBeTruthy());
  fireEvent.press(screen.getByTestId('publish-submit'));
  expect(api.publishRoute).not.toHaveBeenCalled();
});

// «Φεύγω τώρα»: a one-off for the next quarter of an hour, worked out when the
// button is pressed, and the departure opens at once, where «Φεύγω» is.
test('leaving now publishes the next quarter of an hour and opens the departure', async () => {
  jest.useFakeTimers({ now: new Date('2026-10-02T15:00:00Z'), doNotFake: ['setTimeout', 'setInterval'] });
  (api.publishRoute as jest.Mock).mockResolvedValue({
    template_id: 'r1',
    route_km: 28.7,
    ends_at_port: false,
    first_occurrence_id: 'occ-now',
  });
  await pickBothEnds();
  fireEvent.press(screen.getByTestId('schedule-now'));
  expect(screen.queryByTestId('depart-earliest')).toBeNull();

  fireEvent.press(screen.getByTestId('publish-submit'));
  await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());
  expect((api.publishRoute as jest.Mock).mock.calls[0][0]).toMatchObject({
    kind: 'oneoff',
    oneoff_date: '2026-10-02',
    depart_earliest: '18:02',
    depart_latest: '18:17',
  });
  await waitFor(() =>
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/trip/[id]', params: { id: 'occ-now' } }),
  );
  jest.useRealTimers();
});

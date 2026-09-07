import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

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

async function fillRoute(garage = [car]) {
  (api.places as jest.Mock).mockResolvedValue({ places: [pin('Αλεξανδρούπολη', 25.87, 40.84)] });
  (api.vehicles as jest.Mock).mockResolvedValue(garage);
  (api.publishRoute as jest.Mock).mockResolvedValue({
    template_id: 'r1',
    route_km: 31.2,
    ends_at_port: false,
  });

  render(wrap(<PublishScreen />));

  for (const field of ['publish-origin', 'publish-destination']) {
    fireEvent.changeText(screen.getByTestId(`${field}-input`), 'αλεξ');
    await waitFor(() => screen.getByTestId(`${field}-node/Αλεξανδρούπολη`));
    fireEvent.press(screen.getByTestId(`${field}-node/Αλεξανδρούπολη`));
  }
}

beforeEach(() => jest.clearAllMocks());

describe('the schedule is when the car goes, and nothing else', () => {
  // Seats, bags and the fuel used to live in the schedule card, which made
  // "πρόγραμμα" the name of the box holding everything with no home.
  test('a preset replaces the days rather than adding to them', async () => {
    await fillRoute();

    fireEvent.press(screen.getByTestId('preset-presetWeekend'));
    fireEvent.press(screen.getByTestId('publish-submit'));

    await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());
    expect((api.publishRoute as jest.Mock).mock.calls[0][0].weekdays).toEqual([6, 7]);
  });

  test('the preset that matches the chosen days reads as chosen', async () => {
    await fillRoute();

    // Monday to Friday is the form's own default.
    expect(screen.getByTestId('preset-presetWeekdays').props.accessibilityState?.selected).toBe(
      true,
    );
    expect(screen.getByTestId('preset-presetWeekend').props.accessibilityState?.selected).toBe(
      false,
    );
  });

  test('a day can still be dropped one at a time', async () => {
    await fillRoute();

    fireEvent.press(screen.getByTestId('weekday-3'));
    fireEvent.press(screen.getByTestId('publish-submit'));

    await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());
    expect((api.publishRoute as jest.Mock).mock.calls[0][0].weekdays).toEqual([1, 2, 4, 5]);
  });
});

describe('what the driver asks for', () => {
  test('the formula and the whole share are what an untouched form sends', async () => {
    await fillRoute();
    fireEvent.press(screen.getByTestId('publish-submit'));

    await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());
    const body = (api.publishRoute as jest.Mock).mock.calls[0][0];
    expect(body.pricing_mode).toBe('formula');
    expect(body.contribution_share).toBe(1);
  });

  // A fraction of nothing is nothing: an open contribution has no figure for a
  // share to be a share of, so none is sent.
  test('an open contribution sends no share beside it', async () => {
    await fillRoute();

    fireEvent.press(screen.getByTestId('publish-share-free'));
    fireEvent.press(screen.getByTestId('publish-submit'));

    await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());
    const body = (api.publishRoute as jest.Mock).mock.calls[0][0];
    expect(body.pricing_mode).toBe('free_contribution');
    expect(body).not.toHaveProperty('contribution_share');
  });

  // Switching away and back must not quietly put a driver who chose half back
  // on the whole share.
  test('a chosen fraction survives a trip through the open contribution', async () => {
    await fillRoute();

    fireEvent.press(screen.getByTestId('publish-share-50'));
    fireEvent.press(screen.getByTestId('publish-share-free'));
    fireEvent.press(screen.getByTestId('publish-share-auto'));
    fireEvent.press(screen.getByTestId('publish-submit'));

    await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());
    expect((api.publishRoute as jest.Mock).mock.calls[0][0].contribution_share).toBe(0.5);
  });
});

// An empty garage is the reason the server would refuse this, and the screen
// knows it before the button is pressed.
test('a driver with no car is stopped here rather than by the server', async () => {
  await fillRoute([]);

  await waitFor(() =>
    expect(screen.getByTestId('publish-submit').props.accessibilityState?.disabled).toBe(true),
  );
  expect(api.publishRoute).not.toHaveBeenCalled();
});

// What the driver is actually asked, and why there is arithmetic on the screen.
//
// luggage_capacity is compared by the matcher against what passengers have
// already booked (internal/match/gross.go) and against nothing else — the
// driver's own bags are not modelled anywhere. So the number has always had to
// mean "places left for passengers", and a driver reading "πόσες χωράνε" was
// being asked for the whole boot and over-promising by however much they were
// bringing themselves.
//
// The fix is the question, not the schema: the field is labelled as the places
// that remain, and a small calculator fills it in for a driver who would rather
// think in "boot minus my own". The two calculator inputs are not sent and not
// stored, so reopening a route and touching nothing cannot subtract twice.
describe('the boot, minus what the driver is bringing', () => {
  test('total minus own is what gets published', async () => {
    await fillRoute();

    fireEvent.press(screen.getByTestId('boot-total-4'));
    fireEvent.press(screen.getByTestId('boot-own-1'));

    expect(screen.getByTestId('publish-luggage-count-3').props.accessibilityState?.selected).toBe(
      true,
    );

    fireEvent.press(screen.getByTestId('publish-submit'));
    await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());
    expect((api.publishRoute as jest.Mock).mock.calls[0][0].luggage_capacity).toBe(3);
  });

  // Zero places left is a real answer, but it is not one this field can carry:
  // the CHECK in migration 00026 pairs a capacity of zero with the size 'none'.
  // Saying so is better than quietly clamping the driver's own luggage down to
  // leave one place they did not say they had — and better than the earlier
  // behaviour, which said so on screen and then published the stale 3 anyway.
  test('taking the whole boot blocks publishing rather than sending the old number', async () => {
    await fillRoute();

    fireEvent.press(screen.getByTestId('boot-total-2'));
    fireEvent.press(screen.getByTestId('boot-own-2'));

    expect(screen.getByTestId('boot-nothing-left')).toBeTruthy();
    expect(screen.getByTestId('publish-submit')).toBeDisabled();
    fireEvent.press(screen.getByTestId('publish-submit'));
    await waitFor(() => expect(api.publishRoute).not.toHaveBeenCalled());
  });

  // Freeing a place again has to re-open the form: a guard that latches is a
  // driver stuck on a screen with no way forward and no message saying why.
  test('leaving a place free again lets the route go out, with the real number', async () => {
    await fillRoute();

    fireEvent.press(screen.getByTestId('boot-total-2'));
    fireEvent.press(screen.getByTestId('boot-own-2'));
    fireEvent.press(screen.getByTestId('boot-own-1'));

    fireEvent.press(screen.getByTestId('publish-submit'));
    await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());
    expect((api.publishRoute as jest.Mock).mock.calls[0][0].luggage_capacity).toBe(1);
  });

  test('an untouched calculator changes nothing about what is sent', async () => {
    await fillRoute();
    fireEvent.press(screen.getByTestId('publish-submit'));

    await waitFor(() => expect(api.publishRoute).toHaveBeenCalled());
    const body = (api.publishRoute as jest.Mock).mock.calls[0][0];
    expect(body.luggage_capacity).toBe(3);
    expect(body.luggage).toBe('suitcase');
  });
});

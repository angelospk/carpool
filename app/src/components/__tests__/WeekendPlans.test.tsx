import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { WeekendPlans } from '../WeekendPlans';
import { api, type WeekendPlan } from '../../api/endpoints';

const mockPush = jest.fn();
jest.mock('expo-router', () => ({ useRouter: () => ({ push: mockPush }) }));
jest.mock('../../api/endpoints', () => ({
  api: { weekendPlans: jest.fn(), stopWeekendPlan: jest.fn(), weekendThisWeek: jest.fn(), keepWeekendPlan: jest.fn() },
}));
jest.mock('../../state/session', () => ({
  useSession: (select: (s: unknown) => unknown) => select({ status: 'signedIn' }),
}));
// Stopping asks first; the test answers yes at once.
jest.mock('../confirm', () => ({ confirm: ({ onConfirm }: { onConfirm: () => void }) => onConfirm() }));

function wrap(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

let nextId = 0;
const plan = (over: Partial<WeekendPlan> = {}): WeekendPlan => ({
  id: `plan-${++nextId}`,
  origin_lon: 25.87,
  origin_lat: 40.84,
  destination_lon: 22.94,
  destination_lat: 40.64,
  origin_label: 'Αλεξανδρούπολη',
  destination_label: 'Θεσσαλονίκη',
  depart_dow: 5,
  depart_time: '15:00',
  return_dow: null,
  return_time: null,
  seats: 1,
  luggage: 'none',
  luggage_count: 0,
  every_week: true,
  declines_in_row: 0,
  this_week: 'open',
  ask_to_stop: false,
  answered_this_week: false,
  ...over,
});

// Wednesday, so the Thursday question is not due unless a test moves the clock.
const WEDNESDAY = new Date('2026-10-14T09:00:00Z');
const THURSDAY_EVENING = new Date('2026-10-15T16:00:00Z'); // 19:00 in Athens

beforeEach(() => {
  jest.clearAllMocks();
  jest.useFakeTimers({ now: WEDNESDAY, advanceTimers: true });
  (api.weekendPlans as jest.Mock).mockResolvedValue({ plans: [] });
  (api.stopWeekendPlan as jest.Mock).mockResolvedValue(undefined);
  (api.weekendThisWeek as jest.Mock).mockImplementation(async () => plan());
  (api.keepWeekendPlan as jest.Mock).mockImplementation(async () => plan());
});
afterEach(() => jest.useRealTimers());

test('the way in to a new plan is always there', async () => {
  render(wrap(<WeekendPlans />));

  expect(screen.getByText('Απόδραση το Σαββατοκύριακο')).toBeTruthy();
  fireEvent.press(screen.getByTestId('weekend-new'));
  expect(mockPush).toHaveBeenCalledWith('/weekend/new');
});

test('a plan reads as where, which day and when, with how this weekend stands', async () => {
  const p = plan({ return_dow: 0, return_time: '18:00', every_week: false });
  (api.weekendPlans as jest.Mock).mockResolvedValue({ plans: [p] });
  render(wrap(<WeekendPlans />));

  expect(await screen.findByTestId(`weekend-plan-${p.id}-line`)).toHaveTextContent(
    'Σαββατοκύριακο: Αλεξανδρούπολη → Θεσσαλονίκη, Πα 15:00',
  );
  expect(screen.getByText('Ψάχνουμε οδηγό')).toBeTruthy();
  expect(screen.getByText('Επιστροφή: Κυ 18:00')).toBeTruthy();
  expect(screen.getByText('Μόνο αυτό το Σαββατοκύριακο')).toBeTruthy();
  // Wednesday: nothing to answer yet.
  expect(screen.queryByTestId(`weekend-going-ask-${p.id}`)).toBeNull();
});

test('stopping a plan asks first, then stops it', async () => {
  const p = plan();
  (api.weekendPlans as jest.Mock).mockResolvedValue({ plans: [p] });
  render(wrap(<WeekendPlans />));

  fireEvent.press(await screen.findByTestId(`weekend-stop-${p.id}`));

  await waitFor(() => expect(api.stopWeekendPlan).toHaveBeenCalledWith(p.id));
});

describe('on Thursday evening', () => {
  beforeEach(() => jest.setSystemTime(THURSDAY_EVENING));

  test('a plan still looking is asked whether the passenger leaves', async () => {
    const p = plan();
    (api.weekendPlans as jest.Mock).mockResolvedValue({ plans: [p] });
    render(wrap(<WeekendPlans />));

    expect(await screen.findByText('Φεύγεις αυτό το Σαββατοκύριακο;')).toBeTruthy();
    fireEvent.press(screen.getByTestId(`weekend-going-yes-${p.id}`));

    await waitFor(() => expect(api.weekendThisWeek).toHaveBeenCalledWith(p.id, true));
  });

  test('an answer the server already has is not asked for again, after a restart too', async () => {
    const p = plan({ answered_this_week: true });
    (api.weekendPlans as jest.Mock).mockResolvedValue({ plans: [p] });
    render(wrap(<WeekendPlans />));

    await screen.findByTestId(`weekend-plan-${p.id}`);
    expect(screen.queryByTestId(`weekend-going-ask-${p.id}`)).toBeNull();
  });

  test('the question goes once the refetched plan says it is answered', async () => {
    const p = plan();
    (api.weekendPlans as jest.Mock)
      .mockResolvedValueOnce({ plans: [p] })
      .mockResolvedValue({ plans: [{ ...p, answered_this_week: true }] });
    render(wrap(<WeekendPlans />));

    fireEvent.press(await screen.findByTestId(`weekend-going-yes-${p.id}`));
    await waitFor(() => expect(screen.queryByTestId(`weekend-going-ask-${p.id}`)).toBeNull());
  });

  test('«Όχι αυτή τη φορά» says the passenger is not going', async () => {
    const p = plan();
    (api.weekendPlans as jest.Mock).mockResolvedValue({ plans: [p] });
    render(wrap(<WeekendPlans />));

    fireEvent.press(await screen.findByTestId(`weekend-going-no-${p.id}`));

    await waitFor(() => expect(api.weekendThisWeek).toHaveBeenCalledWith(p.id, false));
  });

  test('a plan with nothing looking is not asked', async () => {
    const p = plan({ this_week: 'matched' });
    (api.weekendPlans as jest.Mock).mockResolvedValue({ plans: [p] });
    render(wrap(<WeekendPlans />));

    await screen.findByTestId(`weekend-plan-${p.id}`);
    expect(screen.queryByTestId(`weekend-going-ask-${p.id}`)).toBeNull();
  });

  test('after three «no» in a row the plan asks to be stopped instead', async () => {
    const p = plan({ ask_to_stop: true, declines_in_row: 3 });
    (api.weekendPlans as jest.Mock).mockResolvedValue({ plans: [p] });
    render(wrap(<WeekendPlans />));

    expect(await screen.findByText('Να το σταματήσω;')).toBeTruthy();
    expect(screen.queryByTestId(`weekend-going-ask-${p.id}`)).toBeNull();

    fireEvent.press(screen.getByTestId(`weekend-stop-yes-${p.id}`));
    await waitFor(() => expect(api.stopWeekendPlan).toHaveBeenCalledWith(p.id));
  });

  test('«Όχι» keeps the plan, by the answer that only clears the count', async () => {
    const p = plan({ ask_to_stop: true, declines_in_row: 3 });
    (api.weekendPlans as jest.Mock).mockResolvedValue({ plans: [p] });
    render(wrap(<WeekendPlans />));

    fireEvent.press(await screen.findByTestId(`weekend-stop-no-${p.id}`));

    await waitFor(() => expect(api.keepWeekendPlan).toHaveBeenCalledWith(p.id));
    expect(api.weekendThisWeek).not.toHaveBeenCalled();
    expect(api.stopWeekendPlan).not.toHaveBeenCalled();
  });
});

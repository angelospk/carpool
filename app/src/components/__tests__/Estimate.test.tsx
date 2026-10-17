import { render, screen, waitFor, act, fireEvent } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { Estimate } from '../Estimate';
import { api } from '../../api/endpoints';

jest.mock('../../api/endpoints', () => ({
  api: { estimate: jest.fn() },
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const feres = { lon: 26.174, lat: 40.894, label: 'Φέρες' };
const alex = { lon: 25.874, lat: 40.848, label: 'Αλεξανδρούπολη' };

const estimate = {
  kind: 'planning_estimate' as const,
  route_km: 29,
  route_min: 33,
  contribution_cents: 200,
  fuel_cents: 200,
  toll_cents: 0,
  ceiling_cents: 0,
  assumptions: {
    version: 'eea-2026-08',
    divisor: 4,
    cost_per_km_cents: 9.85,
    fuel_price_as_of: '2026-08-05',
    region_name: 'Περιφερειακή Ενότητα Έβρου',
  },
};

beforeEach(() => {
  jest.clearAllMocks();
  (api.estimate as jest.Mock).mockResolvedValue(estimate);
});

test('it shows a figure once both ends are chosen', async () => {
  render(wrap(<Estimate from={alex} to={feres} testID="est" />));

  expect(await screen.findByTestId('est-headline')).toBeTruthy();
  expect(screen.getByText(/2,00 €/)).toBeTruthy();
});

// The sentence that keeps this from being a price. Without it the screen is
// quoting a figure for a service nobody has offered, so it is never folded away.
test('it says plainly that no driver has agreed to anything', async () => {
  render(wrap(<Estimate from={alex} to={feres} testID="est" />));

  const headline = await screen.findByTestId('est-headline');
  expect(headline.props.children).toMatch(/όχι προσφορά οδηγού/i);
});

// Four paragraphs under the search form was a wall nobody read. One sentence
// shows; how it is worked out is one tap away.
test('the working is folded until asked for', async () => {
  (api.estimate as jest.Mock).mockResolvedValue({ ...estimate, ceiling_cents: 500 });
  render(wrap(<Estimate from={alex} to={feres} testID="est" />));

  const how = await screen.findByTestId('est-how');
  expect(screen.queryByTestId('est-caveat')).toBeNull();
  expect(screen.queryByTestId('est-ceiling')).toBeNull();
  expect(screen.queryByText(/προσφερόμενες/)).toBeNull();

  fireEvent.press(how);
  expect(screen.getByTestId('est-caveat').props.children).toMatch(/δεν έχει βρεθεί/i);

  fireEvent.press(how);
  expect(screen.queryByTestId('est-caveat')).toBeNull();
});

test('it names what it assumed', async () => {
  render(wrap(<Estimate from={alex} to={feres} testID="est" />));

  fireEvent.press(await screen.findByTestId('est-how'));
  // Three seats, from a divisor of four.
  expect(screen.getByText(/29 χλμ.*3 προσφερόμενες/)).toBeTruthy();
});

test('it asks nothing until both ends are chosen', async () => {
  render(wrap(<Estimate from={alex} to={null} testID="est" />));

  await act(async () => {});
  expect(api.estimate).not.toHaveBeenCalled();
  expect(screen.queryByTestId('est')).toBeNull();
});

// A trip that cannot be routed, or a deployment with no fuel prices, shows no
// figure — and no error either, because nothing the person did was wrong.
test('it stays silent rather than showing a number it does not have', async () => {
  (api.estimate as jest.Mock).mockRejectedValue(new Error('no route'));

  render(wrap(<Estimate from={alex} to={feres} testID="est" />));

  await waitFor(() => expect(api.estimate).toHaveBeenCalled());
  await act(async () => {});
  expect(screen.queryByTestId('est')).toBeNull();
});

// Moving a pin asks again, so a figure never sits under a route it was not
// computed for.
test('changing an end asks again', async () => {
  const { rerender } = render(wrap(<Estimate from={alex} to={feres} testID="est" />));
  await screen.findByTestId('est-headline');

  rerender(wrap(<Estimate from={alex} to={{ ...feres, lon: 26.3 }} testID="est" />));
  await waitFor(() => expect(api.estimate).toHaveBeenCalledTimes(2));
});

// The upper bound is what stops a surprise. There is no matching lower figure:
// the thriftiest real car is five times cheaper than the ceiling, and a pair
// that wide reads as "nobody knows".
test('it says how far up the figure could go', async () => {
  (api.estimate as jest.Mock).mockResolvedValue({ ...estimate, ceiling_cents: 500 });

  render(wrap(<Estimate from={alex} to={feres} testID="est" />));

  fireEvent.press(await screen.findByTestId('est-how'));
  const ceiling = screen.getByTestId('est-ceiling');
  // \s rather than a literal space: Intl separates the amount from the symbol
  // with a non-breaking one, which reads identically and compares differently.
  expect(ceiling.props.children).toMatch(/5,00\s€/);
});

test('no upper bound is shown when there is nothing above the estimate', async () => {
  (api.estimate as jest.Mock).mockResolvedValue({ ...estimate, ceiling_cents: 200 });

  render(wrap(<Estimate from={alex} to={feres} testID="est" />));

  fireEvent.press(await screen.findByTestId('est-how'));
  expect(screen.queryByTestId('est-ceiling')).toBeNull();
});

// QA U F4: the search said fuel only while the real ask carried the tolls too.
test('it says tolls are in the figure when the route has them', async () => {
  (api.estimate as jest.Mock).mockResolvedValue({
    ...estimate,
    contribution_cents: 238,
    fuel_cents: 100,
    toll_cents: 138,
  });

  render(wrap(<Estimate from={alex} to={feres} testID="est" />));

  const headline = await screen.findByTestId('est-headline');
  expect(headline.props.children).toMatch(/2,38\s€.*καύσιμα και διόδια.*όχι προσφορά οδηγού/);
});

test('a route without tolls keeps the fuel-only sentence', async () => {
  render(wrap(<Estimate from={alex} to={feres} testID="est" />));

  const headline = await screen.findByTestId('est-headline');
  expect(headline.props.children).toMatch(/για τα καύσιμα/);
  expect(headline.props.children).not.toMatch(/διόδια/);
});

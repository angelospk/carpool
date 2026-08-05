import { render, screen, fireEvent } from '@testing-library/react-native';

import { PriceBreakdown } from '../PriceBreakdown';
import { type PriceBreakdownData } from '../../api/endpoints';

const formula: PriceBreakdownData = {
  v: 1,
  basis_v: 2,
  consumption_l_100km: 5.6,
  consumption_source: 'dataset',
  fuel: 'diesel',
  fuel_price_eur: 1.729,
  region_name: 'Περιφερειακή Ενότητα Έβρου',
  price_is_regional: false,
  trip_km: 103,
  fuel_cost_cents: 997,
  divisor: 4,
  share_cents: 249,
  rounded_cents: 200,
  toll_share_cents: 140,
  total_cents: 340,
  waived: false,
  mode: 'formula',
};

const open = (b: PriceBreakdownData = formula) => {
  render(<PriceBreakdown breakdown={b} testID="pb" />);
  fireEvent.press(screen.getByTestId('pb-open'));
};

// The division is the answer. A total on its own could be anything.
test('the arithmetic is shown step by step', () => {
  open();

  expect(screen.getByTestId('pb-fuel')).toBeTruthy();
  expect(screen.getByTestId('pb-divided')).toBeTruthy();
  expect(screen.getByTestId('pb-rounded')).toBeTruthy();
  expect(screen.getByTestId('pb-tolls')).toBeTruthy();
  expect(screen.getByText('3,40 €')).toBeTruthy();
});

// The divisor is what makes the no-profit argument, so it is said in words
// rather than left as a number somebody has to interpret.
test('the divisor is named as every seat plus the driver', () => {
  open();
  expect(screen.getByText(/Δια 4 .*οδηγ/i)).toBeTruthy();
});

// Nothing on this screen may be recomputed. The component renders what the
// server sent, even when the parts look odd together — a receipt from an older
// formula is exactly that case.
test('the total is the server’s, not the sum of what is drawn', () => {
  open({ ...formula, total_cents: 999 });
  expect(screen.getByText('9,99 €')).toBeTruthy();
});

test('a trip too short to charge for says so instead of showing zeros', () => {
  render(
    <PriceBreakdown
      breakdown={{ ...formula, waived: true, total_cents: 0, rounded_cents: 0 }}
      testID="pb"
    />,
  );
  expect(screen.getByTestId('pb-waived')).toBeTruthy();
  expect(screen.queryByTestId('pb-open')).toBeNull();
});

// A driver who asks for nothing is not a trip too short to charge for, and the
// two must not read the same.
test('a free ride is described as a free ride', () => {
  render(<PriceBreakdown breakdown={{ ...formula, mode: 'free' }} testID="pb" />);
  expect(screen.getByTestId('pb-mode')).toBeTruthy();
  expect(screen.queryByTestId('pb-waived')).toBeNull();
});

// A figure that is a floor for the car's class should not read as a
// measurement, and a national average should not read as a local price.
test('a soft number says that it is soft', () => {
  open({ ...formula, consumption_source: 'band' });
  expect(screen.getByText(/προσέγγιση/i)).toBeTruthy();
  expect(screen.getByText(/εθνικός/i)).toBeTruthy();
});

test('a rounding that changed nothing is not shown as a step', () => {
  open({ ...formula, share_cents: 200, rounded_cents: 200 });
  expect(screen.queryByTestId('pb-rounded')).toBeNull();
});

// The waiver covers the driver's own fuel, not the barrier they handed cash
// over at. A short trip that passed a toll still has a total, and saying "the
// driver asks for nothing" over the top of a number on the same screen is a
// lie about that number.
test('a waived trip that passed a toll shows the toll rather than claiming nothing is owed', () => {
  open({
    ...formula,
    waived: true,
    share_cents: 0,
    rounded_cents: 0,
    toll_share_cents: 140,
    total_cents: 140,
  });

  expect(screen.queryByTestId('pb-waived')).toBeNull();
  expect(screen.getByTestId('pb-tolls')).toBeTruthy();
  // Once as the toll line, once as the total: everything owed is the toll.
  expect(screen.getAllByText('1,40 €')).toHaveLength(2);
});

// And one that passed none says so, which is the case the early return exists
// for.
test('a waived trip with nothing to pay says the driver asks for nothing', () => {
  render(
    <PriceBreakdown
      breakdown={{ ...formula, waived: true, rounded_cents: 0, toll_share_cents: 0, total_cents: 0 }}
      testID="pb"
    />,
  );
  expect(screen.getByTestId('pb-waived')).toBeTruthy();
});

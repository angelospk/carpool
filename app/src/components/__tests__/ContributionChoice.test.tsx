import { render, screen, fireEvent } from '@testing-library/react-native';

import { ContributionChoice } from '../ContributionChoice';

test('every fraction is offered, the whole share first', () => {
  render(<ContributionChoice value={1} onChange={jest.fn()} testID="cc" />);

  for (const pct of [100, 75, 50, 25]) {
    expect(screen.getByTestId(`cc-${pct}`)).toBeTruthy();
  }
});

test('choosing one reports the fraction, not the percentage', () => {
  const onChange = jest.fn();
  render(<ContributionChoice value={1} onChange={onChange} testID="cc" />);

  fireEvent.press(screen.getByTestId('cc-50'));
  expect(onChange).toHaveBeenCalledWith(0.5);
});

test('the whole share reads as chosen by default', () => {
  render(<ContributionChoice value={1} onChange={jest.fn()} testID="cc" />);

  expect(screen.getByTestId('cc-100').props.accessibilityState?.selected).toBe(true);
  expect(screen.getByTestId('cc-50').props.accessibilityState?.selected).toBe(false);
});

// The sentence is the point of the whole component. The driver has to see that
// this is their decision and that the app's role is the cap.
test('it says the driver decides and that the app only caps', () => {
  render(<ContributionChoice value={1} onChange={jest.fn()} testID="cc" />);

  expect(screen.getByText(/Εσύ ορίζεις/)).toBeTruthy();
  expect(screen.getByText(/δεν σε αφήνει να ζητήσεις πάνω/)).toBeTruthy();
  expect(screen.getByText(/κέρδος δεν επιτρέπεται/)).toBeTruthy();
});

// There is no chip above the whole share, because that is the one answer the
// app does not accept.
test('nothing above the whole share is offered', () => {
  render(<ContributionChoice value={1} onChange={jest.fn()} testID="cc" />);

  for (const pct of [125, 150, 200]) {
    expect(screen.queryByTestId(`cc-${pct}`)).toBeNull();
  }
});

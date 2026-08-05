import { render, screen, fireEvent } from '@testing-library/react-native';

import { LuggagePicker } from '../LuggagePicker';
import { LUGGAGE_SIZES } from '../../api/endpoints';

test('every size is offered, smallest first', () => {
  render(<LuggagePicker value="none" onChange={jest.fn()} testID="lp" />);

  for (const size of LUGGAGE_SIZES) {
    expect(screen.getByTestId(`lp-${size}`)).toBeTruthy();
  }
});

test('picking one reports it', () => {
  const onChange = jest.fn();
  render(<LuggagePicker value="none" onChange={onChange} testID="lp" />);

  fireEvent.press(screen.getByTestId('lp-bulky'));
  expect(onChange).toHaveBeenCalledWith('bulky');
});

test('what is chosen reads as chosen', () => {
  render(<LuggagePicker value="suitcase" onChange={jest.fn()} testID="lp" />);

  expect(screen.getByTestId('lp-suitcase').props.accessibilityState?.selected).toBe(true);
  expect(screen.getByTestId('lp-none').props.accessibilityState?.selected).toBe(false);
});

// It is not a preference, and the two sides ask different questions. A driver
// is not being asked what they are carrying.
test('the driver is asked what fits, the passenger what they have', () => {
  const { rerender } = render(<LuggagePicker value="none" onChange={jest.fn()} testID="lp" />);
  expect(screen.getByText(/κουβαλάς/i)).toBeTruthy();

  rerender(<LuggagePicker value="none" onChange={jest.fn()} asDriver testID="lp" />);
  expect(screen.getByText(/χωράει/i)).toBeTruthy();
  expect(screen.queryByText(/κουβαλάς/i)).toBeNull();
});

// The consequence is stated, because it is not the usual one: this chip decides
// who is even offered the trip.
test('it says that the choice narrows who is offered', () => {
  render(<LuggagePicker value="none" onChange={jest.fn()} testID="lp" />);
  expect(screen.getByText(/μόνο οδηγούς/i)).toBeTruthy();
});

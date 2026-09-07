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

describe('how many', () => {
  it('asks for a number once a size has been chosen', () => {
    const onCount = jest.fn();
    render(
      <LuggagePicker testID="lug" value="suitcase" count={1} onChange={jest.fn()} onCountChange={onCount} />,
    );

    fireEvent.press(screen.getByTestId('lug-count-3'));
    expect(onCount).toHaveBeenCalledWith(3);
  });

  // "None" is not a size you can have two of. Leaving the number on screen
  // beside it would invite somebody to set it, and the server refuses that
  // pair.
  it('does not ask how many nothings you are bringing', () => {
    render(
      <LuggagePicker testID="lug" value="none" count={0} onChange={jest.fn()} onCountChange={jest.fn()} />,
    );

    expect(screen.queryByTestId('lug-count-1')).toBeNull();
  });

  // Choosing a size for the first time has to leave a usable pair behind. A
  // size with a count of zero is a row the server will not take.
  it('moves the count with the size', () => {
    const onCount = jest.fn();
    const { rerender } = render(
      <LuggagePicker testID="lug" value="none" count={0} onChange={jest.fn()} onCountChange={onCount} />,
    );

    fireEvent.press(screen.getByTestId('lug-suitcase'));
    expect(onCount).toHaveBeenCalledWith(1);

    onCount.mockClear();
    rerender(
      <LuggagePicker testID="lug" value="suitcase" count={2} onChange={jest.fn()} onCountChange={onCount} />,
    );
    fireEvent.press(screen.getByTestId('lug-none'));
    expect(onCount).toHaveBeenCalledWith(0);
  });
});

// A pictogram beside each word, because "κανονική βαλίτσα" and "ποδήλατο, σκι,
// καρότσι" are a scale rather than a list, and a scale is read faster as a
// picture than as prose. The word stays: an icon alone is a guess, and this
// choice decides who is offered the trip.
describe('the scale is drawn as well as written', () => {
  it('gives every size its own mark, on both sides', () => {
    const { rerender } = render(<LuggagePicker value="none" onChange={jest.fn()} testID="lp" />);

    // includeHiddenElements, because the marks are deliberately hidden from
    // assistive technology — see the next test, which is what makes that a
    // property rather than an accident.
    const mark = (size: string) =>
      screen.getByTestId(`lp-${size}-glyph`, { includeHiddenElements: true })
        .props.children as string;

    const marks = LUGGAGE_SIZES.map(mark);
    expect(new Set(marks).size).toBe(LUGGAGE_SIZES.length);

    rerender(<LuggagePicker value="none" onChange={jest.fn()} asDriver testID="lp" />);
    for (const size of LUGGAGE_SIZES) {
      expect(mark(size)).toBeTruthy();
    }
  });

  // The mark is decoration over a label that already says it. A screen reader
  // announcing "🧳 κανονική βαλίτσα" reads the emoji's own name aloud, which is
  // whatever the platform calls it rather than what this app means by it — so
  // the chip carries the name and the mark is hidden. The default query not
  // finding it is the assertion: that is the same filter a screen reader has.
  it('names the size for a screen reader, and does not read the mark aloud', () => {
    render(<LuggagePicker value="none" onChange={jest.fn()} testID="lp" />);

    expect(screen.getByTestId('lp-suitcase').props.accessibilityLabel).toBe('Κανονική βαλίτσα');
    expect(screen.queryByTestId('lp-suitcase-glyph')).toBeNull();
  });
});

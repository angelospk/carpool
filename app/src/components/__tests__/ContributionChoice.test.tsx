import { render, screen, fireEvent } from '@testing-library/react-native';

import { ContributionChoice } from '../ContributionChoice';

const props = {
  mode: 'formula' as const,
  onModeChange: jest.fn(),
  share: 1,
  onShareChange: jest.fn(),
  testID: 'cc',
};

beforeEach(() => jest.clearAllMocks());

// Two answers, not four fractions. "Τι ζητάς από τα καύσιμα" was the wrong
// question: a driver decides whether a figure is suggested at all first.
test('the two answers are offered, and the automatic one reads as chosen', () => {
  render(<ContributionChoice {...props} />);

  expect(screen.getByTestId('cc-auto').props.accessibilityState?.checked).toBe(true);
  expect(screen.getByTestId('cc-free').props.accessibilityState?.checked).toBe(false);
});

test('choosing the open contribution reports the pricing mode', () => {
  render(<ContributionChoice {...props} />);

  fireEvent.press(screen.getByTestId('cc-free'));
  expect(props.onModeChange).toHaveBeenCalledWith('free_contribution');
});

test('choosing the automatic share reports the formula', () => {
  render(<ContributionChoice {...props} mode="free_contribution" />);

  fireEvent.press(screen.getByTestId('cc-auto'));
  expect(props.onModeChange).toHaveBeenCalledWith('formula');
});

// A fraction of nothing is nothing, so the fractions belong to the one answer
// that produces a figure.
describe('asking for less than the whole share', () => {
  test('the fractions are offered under the automatic share', () => {
    render(<ContributionChoice {...props} />);

    for (const pct of [100, 75, 50, 25]) {
      expect(screen.getByTestId(`cc-${pct}`)).toBeTruthy();
    }
  });

  test('they are gone under an open contribution', () => {
    render(<ContributionChoice {...props} mode="free_contribution" />);

    for (const pct of [100, 75, 50, 25]) {
      expect(screen.queryByTestId(`cc-${pct}`)).toBeNull();
    }
  });

  test('choosing one reports the fraction, not the percentage', () => {
    render(<ContributionChoice {...props} />);

    fireEvent.press(screen.getByTestId('cc-50'));
    expect(props.onShareChange).toHaveBeenCalledWith(0.5);
  });

  test('the whole share reads as chosen by default', () => {
    render(<ContributionChoice {...props} />);

    expect(screen.getByTestId('cc-100').props.accessibilityState?.selected).toBe(true);
    expect(screen.getByTestId('cc-50').props.accessibilityState?.selected).toBe(false);
  });

  // There is no chip above the whole share, because that is the one answer the
  // app does not accept.
  test('nothing above the whole share is offered', () => {
    render(<ContributionChoice {...props} />);

    for (const pct of [125, 150, 200]) {
      expect(screen.queryByTestId(`cc-${pct}`)).toBeNull();
    }
  });
});

// The sentence is the point of the whole component. The driver has to see that
// this is their decision and that the app's role is the cap.
test('it says the driver decides and that the app only caps', () => {
  render(<ContributionChoice {...props} />);

  expect(screen.getByText(/Εσύ ορίζεις/)).toBeTruthy();
  expect(screen.getByText(/δεν σε αφήνει να ζητήσεις πάνω/)).toBeTruthy();
  expect(screen.getByText(/κέρδος δεν επιτρέπεται/)).toBeTruthy();
});

// Nothing is suggested under an open contribution — least of all a figure
// dressed up as a suggestion.
test('the open contribution says no amount is suggested', () => {
  render(<ContributionChoice {...props} mode="free_contribution" />);

  expect(screen.getByText(/Δεν προτείνεται ποσό/)).toBeTruthy();
});

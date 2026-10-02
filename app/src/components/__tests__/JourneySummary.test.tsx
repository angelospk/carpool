import { render, screen } from '@testing-library/react-native';

import { JourneySummary, whenLine } from '../JourneySummary';

describe('the when line', () => {
  it('gives one day and the window', () => {
    // 07:00–08:00 Athens (UTC+3 in summer).
    expect(whenLine('2026-09-29T04:00:00Z', '2026-09-29T05:00:00Z')).toMatch(
      /^.+ · 07:00–08:00$/,
    );
  });

  it('gives one time when the window is a moment', () => {
    expect(whenLine('2026-09-29T04:00:00Z', '2026-09-29T04:00:00Z')).toMatch(/^.+ · 07:00$/);
  });

  // One day followed by two times reads as both on that day, which a window
  // crossing midnight is not.
  it('names both days when the window crosses midnight in Athens', () => {
    const line = whenLine('2026-09-29T20:30:00Z', '2026-09-29T21:30:00Z');
    // 23:30 on the 29th to 00:30 on the 30th, Athens time.
    expect(line).toContain('23:30');
    expect(line).toContain('00:30');
    expect(line).toMatch(/29.*30/);
  });

  it('is empty without a start', () => {
    expect(whenLine(undefined, undefined)).toBe('');
  });
});

describe('JourneySummary', () => {
  it('shows the two places, the when line and the status', () => {
    render(
      <JourneySummary
        testID="j"
        from="Αλεξανδρούπολη"
        to="Κομοτηνή"
        start="2026-09-29T04:00:00Z"
        end="2026-09-29T05:00:00Z"
        status="Ρωτάμε οδηγούς"
        statusTestID="j-status"
      />,
    );

    expect(screen.getByText('Αλεξανδρούπολη')).toBeTruthy();
    expect(screen.getByText('Κομοτηνή')).toBeTruthy();
    expect(screen.getByTestId('j-when')).toHaveTextContent(/07:00–08:00/);
    expect(screen.getByTestId('j-status')).toHaveTextContent('Ρωτάμε οδηγούς');
  });

  it('leaves out what it was not given', () => {
    render(<JourneySummary testID="j" from="Α" to="Β" />);

    expect(screen.queryByTestId('j-when')).toBeNull();
    expect(screen.queryByTestId('j-status')).toBeNull();
  });
});

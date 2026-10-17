import { render, screen, fireEvent } from '@testing-library/react-native';

import { WhenPicker } from '../WhenPicker';

const NOW = new Date('2026-10-17T09:00:00Z'); // Saturday, 12:00 in Athens

// QA 2026-10-17 U F6: "other days" opened with nothing chosen did nothing, and
// tapping a chosen day again quietly took it off. What will be asked for is
// now said in words under the days.
test('the other days say which are chosen, or that none is', () => {
  const onExtra = jest.fn();
  const { rerender } = render(
    <WhenPicker day="tomorrow" band="morning" onChange={jest.fn()} extra={[]} onExtraChange={onExtra} now={NOW} testID="w" />,
  );
  fireEvent.press(screen.getByTestId('w-more'));
  expect(screen.getByTestId('w-extra-summary').props.children).toMatch(/Διάλεξε/);

  rerender(
    <WhenPicker
      day="tomorrow"
      band="morning"
      onChange={jest.fn()}
      extra={['2026-10-25']}
      onExtraChange={onExtra}
      now={NOW}
      testID="w"
    />,
  );
  expect(screen.getByTestId('w-extra-summary').props.children).toMatch(/25 Οκτ/);
});

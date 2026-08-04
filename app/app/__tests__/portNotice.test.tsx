import { render, screen } from '@testing-library/react-native';

import { PortNotice } from '../../src/components/PortNotice';
import el from '../../src/i18n/el.json';

// The ordinary trip. A notice on every trip would be noise, and noise is what
// people stop reading before the one time it matters.
test('a trip that ends where it says it ends says nothing', () => {
  render(<PortNotice endsAtPort={false} />);
  expect(screen.queryByTestId('port-notice')).toBeNull();
});

test('a trip cut short at a ferry explains itself', () => {
  render(<PortNotice endsAtPort />);
  expect(screen.getByTestId('port-notice')).toBeTruthy();
});

// The two things a passenger has to be told: where it stops, and that the boat
// is not part of what they are paying for.
test('the notice says the trip ends at the port and that the ferry is not included', () => {
  render(<PortNotice endsAtPort />);

  const text = screen.getByTestId('port-notice').props.children;
  expect(el.trip.portTitle).toMatch(/λιμάν/i);
  expect(el.trip.portBody).toMatch(/πλοίο|φέρι|εισιτήρι/i);
  expect(text).toBeTruthy();
});

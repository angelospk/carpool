import { destinationOf } from '../destination';

// The server puts the id of whatever a notification is about into its data,
// so a tap can open that rather than the front page.
test('a question for a driver opens the question', () => {
  expect(destinationOf({ ask_id: 'a-1', idempotency_key: 'k' })).toBe('/ask/a-1');
});

// A booking or an arrangement has no screen of its own; the home screen is
// where both are listed, and it refetches on focus.
test('anything else opens home', () => {
  expect(destinationOf({ booking_id: 'b-1' })).toBe('/(tabs)');
  expect(destinationOf({ arrangement_id: 'x' })).toBe('/(tabs)');
});

test('an id that is not a plain id goes nowhere odd', () => {
  expect(destinationOf({ ask_id: '../../admin' })).toBe('/(tabs)');
  expect(destinationOf({ ask_id: 42 })).toBe('/(tabs)');
  expect(destinationOf(undefined)).toBeNull();
});

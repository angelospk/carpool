import { pointLabel } from '../pin';

const alex = { lon: 25.874, lat: 40.848, label: 'Αλεξανδρούπολη' };

test('puts the town before the landmark', () => {
  expect(pointLabel(alex, { name: 'ΚΤΕΛ' })).toBe('Αλεξανδρούπολη, ΚΤΕΛ');
});

test('does not say the town twice when the landmark already starts with it', () => {
  expect(pointLabel(alex, { name: 'Αλεξανδρούπολη Λιμάνι' })).toBe('Αλεξανδρούπολη Λιμάνι');
});

test('does not say the town twice when the landmark carries it as a prefix', () => {
  expect(pointLabel(alex, { name: 'Αλεξανδρούπολη, Λιμάνι' })).toBe('Αλεξανδρούπολη, Λιμάνι');
});

test('does not say the town twice when the landmark has it inside', () => {
  expect(pointLabel(alex, { name: 'Λιμάνι Αλεξανδρούπολης' })).toBe('Λιμάνι Αλεξανδρούπολης');
});

test('is the town alone without a landmark', () => {
  expect(pointLabel(alex, null)).toBe('Αλεξανδρούπολη');
});

import { colors, initials, tileColor } from './tokens';

// A place that changes colour between the list and the detail screen reads as a
// different place. The colour is derived from the label, not assigned at random.
test('a label always gets the same tile colour', () => {
  expect(tileColor('Αλεξανδρούπολη')).toBe(tileColor('Αλεξανδρούπολη'));
  expect(colors.tiles).toContain(tileColor('Κομοτηνή'));
});

test('different labels generally differ', () => {
  const used = new Set(['Αλεξανδρούπολη', 'Κομοτηνή', 'Ορεστιάδα', 'Διδυμότειχο'].map(tileColor));

  expect(used.size).toBeGreaterThan(1);
});

test('initials handle Greek names, one word, and nothing at all', () => {
  expect(initials('Χαράλαμπος Ροβάς')).toBe('ΧΡ');
  expect(initials('Μαρία')).toBe('Μ');
  expect(initials('  ')).toBe('·');
});

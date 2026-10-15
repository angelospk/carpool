import { areaLabel } from './area';

// The place list names a municipality for most villages and a whole regional
// unit for the rest. Spelled out, the unit is three times as long as its
// neighbours and crowds the place name off the row.
test('regional and metropolitan units are shortened the way signs do', () => {
  expect(areaLabel('Περιφερειακή Ενότητα Έβρου')).toBe('Π.Ε. Έβρου');
  expect(areaLabel('Μητροπολιτική Ενότητα Θεσσαλονίκης')).toBe('Μ.Ε. Θεσσαλονίκης');
});

test('a municipality is left as it is', () => {
  expect(areaLabel('Κομοτηνής')).toBe('Κομοτηνής');
  expect(areaLabel('')).toBe('');
});

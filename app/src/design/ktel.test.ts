import { ktelFor } from './ktel';

// The bus a stranded passenger can still take leaves from the town they were
// leaving from, so the timetable is that prefecture's.
test('a town in Ροδόπη gets ΚΤΕΛ Ροδόπης', () => {
  expect(ktelFor('Κομοτηνή')).toEqual({ name: 'Ροδόπης', url: 'https://ktelrodopis.gr/' });
  expect(ktelFor('Σάπες')).toEqual({ name: 'Ροδόπης', url: 'https://ktelrodopis.gr/' });
});

test('a town in Ξάνθη gets ΚΤΕΛ Ξάνθης', () => {
  expect(ktelFor('Ξάνθη')).toEqual({ name: 'Ξάνθης', url: 'https://www.ktelxanthis.gr/' });
});

test('the match ignores case and accents', () => {
  expect(ktelFor('ΚΟΜΟΤΗΝΗ').name).toBe('Ροδόπης');
  expect(ktelFor('xanthi').name).toBe('Ξάνθης');
});

test('anywhere else keeps ΚΤΕΛ Έβρου, where the pilot started', () => {
  expect(ktelFor('Αλεξανδρούπολη')).toEqual({ name: 'Έβρου', url: 'https://ktelevrou.gr/' });
  expect(ktelFor('Φέρες').name).toBe('Έβρου');
});

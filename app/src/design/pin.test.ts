import { parsePin } from './pin';

// "lon, lat Label" is the pilot's stand-in for a map picker. It was parsed in
// two screens with two slightly different regexes; it is one function now.
test('reads a longitude, a latitude and a label', () => {
  expect(parsePin('25.87, 40.85 Αλεξανδρούπολη')).toEqual({
    lon: 25.87,
    lat: 40.85,
    label: 'Αλεξανδρούπολη',
  });
});

test('falls back to the whole text when no label is typed', () => {
  expect(parsePin('25.87, 40.85')).toEqual({ lon: 25.87, lat: 40.85, label: '25.87, 40.85' });
});

test('rejects anything that is not a pair of numbers', () => {
  expect(parsePin('Αλεξανδρούπολη')).toBeNull();
  expect(parsePin('')).toBeNull();
  expect(parsePin('25.87')).toBeNull();
});

// Coordinates the server would reject anyway are better refused on the device,
// where the user can still see which field is wrong.
test('rejects coordinates off the globe, and the pair the other way round', () => {
  expect(parsePin('200, 40.85 Λάθος')).toBeNull();
  expect(parsePin('25.87, 99 Λάθος')).toBeNull();
});

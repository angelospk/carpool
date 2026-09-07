import { parsePin, pinsFromParams } from './pin';

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

describe('pins carried in a link', () => {
  const complete = {
    from_lon: '25.8743',
    from_lat: '40.8476',
    from_label: 'Αλεξανδρούπολη',
    to_lon: '25.4054',
    to_lat: '41.1223',
    to_label: 'Κομοτηνή',
  };

  test('a complete pair arrives as two pins', () => {
    expect(pinsFromParams(complete)).toEqual({
      from: { lon: 25.8743, lat: 40.8476, label: 'Αλεξανδρούπολη' },
      to: { lon: 25.4054, lat: 41.1223, label: 'Κομοτηνή' },
    });
  });

  // Half a preset fills half a form, which reads as a form somebody started
  // and abandoned.
  test.each([
    ['a missing end', { ...complete, to_lat: undefined }],
    ['a label that is only spaces', { ...complete, from_label: '   ' }],
    ['a coordinate that is not a number', { ...complete, from_lon: 'κάπου' }],
    ['a coordinate off the planet', { ...complete, to_lat: '412' }],
  ])('%s is no preset at all', (_name, params) => {
    expect(pinsFromParams(params as Record<string, string | undefined>)).toBeNull();
  });

  test('a repeated parameter takes the first', () => {
    expect(pinsFromParams({ ...complete, from_label: ['Φέρες', 'Σουφλί'] })?.from.label).toBe(
      'Φέρες',
    );
  });

  test('nothing in the link is no preset', () => {
    expect(pinsFromParams({})).toBeNull();
  });
});

import '../../i18n';

import { ApiError } from '../client';
import { refusalText } from '../errorText';

// The search screen checked for "outside_region", a code the server has never
// sent, so a pin across the border was answered with a generic error.
test('a pin outside the country gets a sentence, not a generic error', () => {
  const text = refusalText(new ApiError(400, 'outside_country', 'origin is outside Greece'));
  expect(text).toMatch(/Ελλάδα/);
});

test('an unknown code is left to the caller', () => {
  expect(refusalText(new ApiError(400, 'something_new', 'x'))).toBeNull();
});

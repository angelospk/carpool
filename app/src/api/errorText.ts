// The sentence to show for a refusal the server named.
//
// The server's own message is English and written for whoever reads the logs.
// The code is the contract, so a code this file knows gets a Greek sentence
// that says what to change; anything else falls back to whatever the screen
// would have said before.

import i18n from 'i18next';

import { ApiError } from './client';

const known = new Set([
  'invalid_seats',
  'oneoff_in_past',
  'oneoff_too_far',
  'invalid_window',
  'too_short',
  'window_in_past',
  'window_too_far',
  'already_aboard',
  'invalid_name',
  'women_only_needs_female',
  'rate_limited',
  'outside_country',
  'bad_departure',
  'outside_window',
  'drive_departed',
  'trip_not_running',
  'bad_detour',
]);

/** A sentence for a refusal this app knows, or null for the caller to decide. */
export function refusalText(e: unknown): string | null {
  if (e instanceof ApiError && e.code && known.has(e.code)) return i18n.t(`errors.${e.code}`);
  return null;
}

// Where a tapped notification should land.
//
// The server attaches the id of whatever the notification is about. An ask
// and a nudged departure have screens of their own; bookings and arrangements
// are listed on the home screen. The id is checked for shape before it becomes part of a path,
// because the payload is data from outside the app.

const ID = /^[0-9a-fA-F-]{1,64}$/;

export function destinationOf(data: Record<string, unknown> | undefined | null): string | null {
  if (!data) return null;
  const ask = data.ask_id;
  if (typeof ask === 'string' && ID.test(ask)) return `/ask/${ask}`;
  // A nudge is about one of the driver's own departures: open it, where the
  // «Φεύγω» card is.
  const occurrence = data.occurrence_id;
  if (typeof occurrence === 'string' && ID.test(occurrence)) return `/trip/${occurrence}`;
  return '/(tabs)';
}

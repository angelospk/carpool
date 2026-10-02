// Where a tapped notification should land.
//
// The server attaches the id of whatever the notification is about. Only an
// ask has a screen of its own; bookings and arrangements are listed on the
// home screen. The id is checked for shape before it becomes part of a path,
// because the payload is data from outside the app.

const ID = /^[0-9a-fA-F-]{1,64}$/;

export function destinationOf(data: Record<string, unknown> | undefined | null): string | null {
  if (!data) return null;
  const ask = data.ask_id;
  if (typeof ask === 'string' && ID.test(ask)) return `/ask/${ask}`;
  return '/(tabs)';
}

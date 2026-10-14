// Which of the upcoming trips the home list shows.
//
// A recurring route is materialised three weeks ahead, so one weekday commute
// is fifteen rows. The list keeps the coming seven days and counts the rest,
// except a seat somebody holds as a passenger: that is never hidden.

const WEEK_MS = 7 * 24 * 60 * 60 * 1000;

type Upcoming = { role: 'driver' | 'passenger'; depart_earliest: string };

export function thisWeek<T extends Upcoming>(trips: T[], now: Date): { shown: T[]; later: number } {
  const edge = now.getTime() + WEEK_MS;
  const shown = trips.filter((x) => x.role === 'passenger' || Date.parse(x.depart_earliest) <= edge);
  return { shown, later: trips.length - shown.length };
}

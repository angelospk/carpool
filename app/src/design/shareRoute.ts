// The sentence a driver pastes into the village's chat group.
//
// There is no listing and no link to send, so the message itself has to carry
// the four things somebody decides on without opening anything: where from,
// where to, when, and whether there is room. It ends with what to do about
// it, because a route with nothing asking for a reply is a fact nobody acts
// on, and the reply channel is the chat the message was pasted into.
//
// Malformed data says less rather than something false: a missing schedule or
// seat count drops its segment instead of leaving a gap or a wrong promise.

export type ShareableRoute = {
  origin_label: string;
  destination_label: string;
  kind: string;
  weekdays?: number[];
  oneoff_date?: string;
  depart_earliest: string;
  seats: number;
};

type Translate = (key: string, options?: { count?: number }) => string;

export function shareRouteMessage(route: ShareableRoute, t: Translate): string {
  const origin = route.origin_label.trim();
  const destination = route.destination_label.trim();
  const where = origin && destination ? `${origin} → ${destination}` : origin || destination;

  // The kind decides which schedule field counts: a one-off keeps its date
  // even if weekdays were left behind by an earlier edit, and a recurring
  // route never borrows a leftover date.
  let when = '';
  if (route.kind === 'oneoff') {
    // Trimmed for the same reason the labels are: a field that arrived as
    // spaces is absent, and untrimmed it passes the filter below and leaves a
    // gap between two separators.
    when = route.oneoff_date?.trim() ?? '';
  } else {
    // A preset is a set, so the days are de-duplicated before comparing: a
    // list that repeats itself is still the same days.
    const days = [...new Set(route.weekdays ?? [])].sort((a, b) => a - b);
    if (days.length > 0) {
      if (equals(days, [1, 2, 3, 4, 5])) when = t('share.weekdays');
      else if (equals(days, [6, 7])) when = t('share.weekend');
      else if (equals(days, [1, 2, 3, 4, 5, 6, 7])) when = t('share.everyday');
      else when = days.map((d) => t(`weekdaysShort.${d}`)).join(' ');
    }
  }

  // The schedule and the time are one thought, not two separated items.
  const whenAndTime = [when, route.depart_earliest.trim()].filter(Boolean).join(' ');
  // Unreachable for a published route: route_templates.seats is CHECK (BETWEEN
  // 1 AND 8) and the publish stepper has a floor of one. It is here for the
  // caller who eventually passes live availability rather than the offered
  // capacity, because at that point a full car would otherwise advertise a
  // seat — and a message that says only where and when is still worth sending.
  const seats = route.seats >= 1 ? t('share.seats', { count: route.seats }) : '';
  const line1 = [where, whenAndTime, seats].filter(Boolean).join(' · ');
  // A route with nothing usable to say about itself still asks to be replied
  // to, but the ask must not arrive underneath a blank first line.
  return [line1, t('share.callToAction')].filter(Boolean).join('\n');
}

function equals(days: number[], preset: number[]): boolean {
  return days.length === preset.length && preset.every((d, i) => days[i] === d);
}

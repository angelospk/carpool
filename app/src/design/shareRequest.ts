// The sentence a passenger pastes into the village's chat group.
//
// The driver's share says "I have seats"; this is the other half, "I need
// one", in the same shape so the two read as a pair in the same chat. The reply
// channel is that chat, so there is no link, and nothing in it names the person
// beyond whoever pasted it.
//
// Malformed data says less rather than something false, as in shareRoute: a
// missing place drops out instead of leaving an arrow pointing at nothing.

import { whenLine } from '../components/JourneySummary';

export type ShareableRequest = {
  origin_label: string;
  destination_label: string;
  window_start: string;
  window_end: string;
  seats: number;
};

type Translate = (key: string, options?: { count?: number }) => string;

export function shareRequestMessage(request: ShareableRequest, t: Translate): string {
  const origin = request.origin_label.trim();
  const destination = request.destination_label.trim();
  const where = origin && destination ? `${origin} → ${destination}` : origin || destination;
  const line1 = [
    t('shareRequest.lookingFor', { count: Math.max(1, request.seats) }),
    where,
    whenLine(request.window_start, request.window_end),
  ]
    .filter(Boolean)
    .join(' · ');
  return [line1, t('shareRequest.callToAction')].join('\n');
}

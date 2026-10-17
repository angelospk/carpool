// What colour each kind of marker is, shared by the browser and the phone so
// the two cannot disagree about which dot is the car.

import { colors } from '../design/tokens';
import type { MapMarker } from './Map';

export function markerColour(m: Pick<MapMarker, 'kind' | 'stale'>): string {
  if (m.stale) return colors.muted;
  switch (m.kind) {
    case 'driver':
      return colors.ink;
    case 'me':
      return colors.accent;
    case 'meeting':
      return colors.danger;
  }
}

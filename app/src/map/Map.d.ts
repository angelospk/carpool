// The contract both maps satisfy.
//
// Map.web.tsx and Map.native.tsx implement it; Metro picks one by platform and
// ignores this file, while TypeScript reads this one — so a screen is checked
// against the shape rather than against whichever platform it resolved to.

import type { ReactElement } from 'react';

export type MapPin = { lon: number; lat: number };

export declare function Map(props: {
  /** Where to look. */
  centre: MapPin;
  /** Pins to draw. The first is the one being moved, when onPick is given. */
  pins?: MapPin[];
  /** An encoded polyline to draw as a line, for a trip's route. */
  polyline?: string;
  /** When given, tapping the map moves the pin and reports where. */
  onPick?: (pin: MapPin) => void;
  testID?: string;
}): ReactElement | null;

// The contract both maps satisfy.
//
// Map.web.tsx and Map.native.tsx implement it; Metro picks one by platform and
// ignores this file, while TypeScript reads this one — so a screen is checked
// against the shape rather than against whichever platform it resolved to.

import type { ReactElement } from 'react';

export type MapPin = { lon: number; lat: number };

/** A labelled dot, for a map that tells people apart: the car, me, the meeting point. */
export type MapMarker = MapPin & {
  kind: 'driver' | 'me' | 'meeting';
  /** Drawn grey: the position is old and no longer moves. */
  stale?: boolean;
};

export declare function Map(props: {
  /** Where to look. */
  centre: MapPin;
  /** Pins to draw. The first is the one being moved, when onPick is given. */
  pins?: MapPin[];
  /** Dots that say which is which, drawn over the plain pins. */
  markers?: MapMarker[];
  /** An encoded polyline to draw as a line, for a trip's route. */
  polyline?: string;
  /** More encoded polylines, drawn the same way: the cars on the road now. */
  lines?: string[];
  /** How far in to start. 13 is a town; 8 is the whole of Evros. */
  zoom?: number;
  /** When given, tapping the map moves the pin and reports where. */
  onPick?: (pin: MapPin) => void;
  testID?: string;
}): ReactElement | null;

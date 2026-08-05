// A stand-in for @maplibre/maplibre-react-native under jest.
//
// It is a native module: outside a built app the registry has nothing to give
// it and importing it throws before any test runs. What the tests are about is
// which of the two things a screen mounts — the map or the "no map configured"
// notice — and that a tap on the map reports where it landed.

import { Pressable, View } from 'react-native';
import type { ReactNode } from 'react';

/** Where a tap on the stub map lands. Somewhere in Φέρες, so a test asserting
    that the pin travelled has a coordinate to compare against. */
export const STUB_TAP: [number, number] = [26.1701, 40.8912];

export function Map({
  children,
  onPress,
}: {
  children?: ReactNode;
  onPress?: (e: { nativeEvent: { lngLat: [number, number] } }) => void;
}) {
  return (
    <View>
      <Pressable
        testID="stub-map-canvas"
        onPress={() => onPress?.({ nativeEvent: { lngLat: STUB_TAP } })}
      />
      {children}
    </View>
  );
}

export function Camera() {
  return null;
}
export type CameraRef = { jumpTo: (o: unknown) => void };
export function Marker({ children }: { children?: ReactNode }) {
  return <>{children}</>;
}
export function GeoJSONSource({ children }: { children?: ReactNode }) {
  return <>{children}</>;
}
export function Layer() {
  return null;
}

export type PressEvent = { lngLat: [number, number]; point: { x: number; y: number } };

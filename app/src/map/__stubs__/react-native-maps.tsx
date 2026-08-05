// A stand-in for react-native-maps under jest.
//
// It is a native module: outside a built app the registry has nothing to give
// it and importing it throws before any test runs. What the tests here are
// about is which of the two things a screen mounts — the map or the "no map
// configured" notice — so the map is a View with the right testID and nothing
// behind it.

import { Pressable, View } from 'react-native';
import type { ReactNode } from 'react';

/** Where a tap on the stub map lands. Somewhere in Φέρες, so a test asserting
    that the pin travelled has a coordinate to compare against. */
export const STUB_TAP = { latitude: 40.8912, longitude: 26.1701 };

export default function MapView({
  children,
  onPress,
}: {
  children?: ReactNode;
  onPress?: (e: { nativeEvent: { coordinate: typeof STUB_TAP } }) => void;
}) {
  return (
    <View>
      <Pressable
        testID="stub-map-canvas"
        onPress={() => onPress?.({ nativeEvent: { coordinate: STUB_TAP } })}
      />
      {children}
    </View>
  );
}

export function Marker() {
  return null;
}
export function Polyline() {
  return null;
}
export function UrlTile() {
  return null;
}

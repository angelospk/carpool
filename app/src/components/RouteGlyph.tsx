// The signature mark: a route drawn as its own shape.
//
// The geometry lives in src/design/routeGlyph.ts so it can be tested on its own;
// this file only puts a stroke on it.

import { View } from 'react-native';
import Svg, { Circle, Path } from 'react-native-svg';
import polyline from '@mapbox/polyline';

import { colors, radius, tileColor } from '../design/tokens';
import { glyphPath, type LatLon } from '../design/routeGlyph';

export function RouteGlyph({
  points,
  seed,
  width = 96,
  height = 72,
  testID,
}: {
  /** Either decoded points, or an encoded polyline via `RouteGlyphEncoded`. */
  points: readonly LatLon[];
  /** Tints the plate, so a route keeps the same colour everywhere it appears. */
  seed: string;
  width?: number;
  height?: number;
  testID?: string;
}) {
  const box = { width, height, padding: 10 };
  const d = glyphPath(points, box);
  const ends = d ? [...d.matchAll(/([\d.]+) ([\d.]+)/g)] : [];
  const first = ends[0];
  const last = ends[ends.length - 1];

  return (
    <View
      testID={testID}
      style={{
        width,
        height,
        borderRadius: radius.inner,
        backgroundColor: tileColor(seed),
        overflow: 'hidden',
      }}
    >
      <Svg width={width} height={height}>
        {d ? (
          <>
            <Path
              d={d}
              stroke={colors.primary}
              strokeWidth={2.5}
              strokeLinecap="round"
              strokeLinejoin="round"
              fill="none"
            />
            <Circle cx={+first[1]} cy={+first[2]} r={3.5} fill={colors.surface} stroke={colors.primary} strokeWidth={2} />
            <Circle cx={+last[1]} cy={+last[2]} r={4} fill={colors.accent} />
          </>
        ) : null}
      </Svg>
    </View>
  );
}

/** Decodes the server's polyline. Returns nothing drawable if it is malformed. */
export function decodeRoute(encoded: string | undefined | null): LatLon[] {
  if (!encoded) return [];
  try {
    return polyline.decode(encoded) as LatLon[];
  } catch {
    return [];
  }
}

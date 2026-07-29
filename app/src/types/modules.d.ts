// @mapbox/polyline ships no types. Only decode is used, and only for the route
// glyph, so the surface is declared here rather than pulling in a stub package.
declare module '@mapbox/polyline' {
  export function decode(encoded: string, precision?: number): [number, number][];
  export function encode(coordinates: [number, number][], precision?: number): string;
  const polyline: { decode: typeof decode; encode: typeof encode };
  export default polyline;
}

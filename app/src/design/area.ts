// How a place's area reads in a suggestion row.
//
// The area is the municipality for most places and the regional unit for the
// rest (the OSM data has no municipality there). Spelled out, a regional unit
// is long enough to push the place name off a phone's row, so it is written
// the way Greek signs and documents write it.

const UNITS: [string, string][] = [
  ['Περιφερειακή Ενότητα ', 'Π.Ε. '],
  ['Μητροπολιτική Ενότητα ', 'Μ.Ε. '],
];

export function areaLabel(area: string): string {
  for (const [long, short] of UNITS) {
    if (area.startsWith(long)) return short + area.slice(long.length);
  }
  return area;
}

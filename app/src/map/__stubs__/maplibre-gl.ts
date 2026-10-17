// A stand-in for maplibre-gl under jest.
//
// The real library draws with WebGL, which jsdom does not have, and ships ESM
// that jest's resolver will not load. Neither is what any test here is about:
// what is worth asserting is which of the two things a screen mounts, the map
// or the "no map configured" notice, and that the map is created once rather
// than rebuilt on every render.
//
// Wired in through moduleNameMapper rather than jest.mock in each file, so a
// screen test that happens to render a map does not have to know.

export class Map {
  on() {}
  once() {}
  remove() {}
  setCenter() {}
  getSource(): unknown {
    return null;
  }
  getLayer(): unknown {
    return null;
  }
  removeLayer() {}
  removeSource() {}
  off() {}
  addSource() {}
  addLayer() {}
  isStyleLoaded() {
    return false;
  }
}

export class Marker {
  setLngLat() {
    return this;
  }
  getElement() {
    return null;
  }
  addTo() {
    return this;
  }
  remove() {}
}

export class GeoJSONSource {}

export type MapMouseEvent = { lngLat: { lng: number; lat: number } };

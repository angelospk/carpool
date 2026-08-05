// Where the map comes from, and why there is not one by default.
//
// The app draws nothing until somebody configures a tile source. That is not an
// oversight: OpenStreetMap's own tile servers are for OSM's own site and their
// policy asks applications not to use them, so an app that shipped pointing at
// tile.openstreetmap.org would be freeloading on a charity's bandwidth and
// would be cut off. Every alternative — MapTiler, Stadia, Protomaps — has a
// free tier and wants a key, and a key belongs to whoever deploys this, not to
// a file in the repository.
//
// So: one environment variable holding an XYZ template, and no map without it.
// Raster rather than vector because it is the one shape every provider offers
// and the one both platforms can draw with what is already installed.
//
//   EXPO_PUBLIC_MAP_TILES_URL=https://tiles.example/{z}/{x}/{y}.png?key=…
//
// The attribution travels with it, because every one of those providers
// requires it on the map itself and OSM's licence requires crediting the
// contributors whatever the provider.

// Read when asked rather than captured at import. Expo inlines these at build
// time either way, so nothing is lost — and it means a test can decide what is
// configured without reloading the module, which under jest would hand it a
// second copy of React and break every hook in the component.
export function tileURL() {
  return (process.env.EXPO_PUBLIC_MAP_TILES_URL ?? '').trim();
}

export function attribution() {
  return process.env.EXPO_PUBLIC_MAP_ATTRIBUTION ?? '© OpenStreetMap contributors';
}

/** Whether a map can be drawn at all. */
export function haveTiles() {
  return tileURL().length > 0;
}

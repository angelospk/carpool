import { render, screen } from '@testing-library/react-native';

// Metro resolves .web/.native by platform; jest-expo runs the web variant,
// which is the one this suite is about.
import { Map } from '../Map.web';

function configure(tiles?: string) {
  if (tiles === undefined) delete process.env.EXPO_PUBLIC_MAP_TILES_URL;
  else process.env.EXPO_PUBLIC_MAP_TILES_URL = tiles;
}

const centre = { lon: 22.9444, lat: 40.6401 };

afterEach(() => configure());

// The default. OpenStreetMap's own servers are for OSM's own site, every
// alternative wants a key, and a key belongs to whoever deploys this — so an
// unconfigured app draws no map, and says so rather than showing a grey box.
test('with no tile source there is no map, and it says why', () => {
  configure();

  render(<Map centre={centre} testID="map" />);

  expect(screen.getByTestId('map')).toBeTruthy();
  expect(screen.getByText(/χάρτης/i)).toBeTruthy();
});

test('an empty tile source counts as none', () => {
  configure('   ');

  render(<Map centre={centre} testID="map" />);
  expect(screen.getByText(/χάρτης/i)).toBeTruthy();
});

// And with one configured, the map itself is mounted rather than the notice.
test('with a tile source the map is drawn', () => {
  configure('https://tiles.example/{z}/{x}/{y}.png');

  render(<Map centre={centre} testID="map" />);

  expect(screen.getByTestId('map')).toBeTruthy();
  expect(screen.queryByText(/χάρτης/i)).toBeNull();
});

// The style is built here rather than fetched, which means it is also the one
// place a wrong tile URL could hide. Both platforms draw from this function, so
// a test on it covers the browser and the phone at once.
describe('the style', () => {
  it('draws from the configured template, and credits it', () => {
    configure('https://tiles.example/{z}/{x}/{y}.png?key=abc');
    process.env.EXPO_PUBLIC_MAP_ATTRIBUTION = '© MapTiler © OpenStreetMap contributors';

    const { rasterStyle } = require('../tiles') as typeof import('../tiles');
    const style = rasterStyle();

    expect(style.sources.base.tiles).toEqual([
      'https://tiles.example/{z}/{x}/{y}.png?key=abc',
    ]);
    expect(style.sources.base.attribution).toContain('MapTiler');
    expect(style.layers[0]).toMatchObject({ type: 'raster', source: 'base' });

    delete process.env.EXPO_PUBLIC_MAP_ATTRIBUTION;
  });

  // The one URL that must never be a default: OSM's policy asks applications
  // not to use their tile servers, and an app that shipped pointing there would
  // be freeloading on a charity's bandwidth until it was blocked.
  it('never falls back to OpenStreetMap’s own servers', () => {
    configure();
    const { rasterStyle, haveTiles } = require('../tiles') as typeof import('../tiles');

    expect(haveTiles()).toBe(false);
    expect(JSON.stringify(rasterStyle())).not.toContain('tile.openstreetmap.org');
  });
});

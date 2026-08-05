// The map, in a browser.
//
// maplibre-gl draws it from a style built here rather than fetched, because the
// one thing configured is an XYZ template and a style around a raster source is
// six lines. A provider that offers vector tiles can be pointed at through the
// same variable; it will simply be drawn as raster.
//
// The map is created once and then told about changes, rather than being
// rebuilt on every render: recreating it drops the user's pan and zoom, which
// on a pin-dropping screen means the pin they were about to place jumps back to
// the middle.

import { useEffect, useRef } from 'react';
import { StyleSheet, View } from 'react-native';
import { GeoJSONSource, Map as GLMap, Marker, type MapMouseEvent } from 'maplibre-gl';
import polyline from '@mapbox/polyline';

import { attribution, haveTiles, tileURL } from './tiles';
import { MapFallback } from './Fallback';
import type { MapPin } from './Map';

const styleFor = (url: string) => ({
  version: 8 as const,
  sources: {
    base: {
      type: 'raster' as const,
      tiles: [url],
      tileSize: 256,
      attribution: attribution(),
    },
  },
  layers: [{ id: 'base', type: 'raster' as const, source: 'base' }],
});

export function Map({
  centre,
  pins = [],
  polyline: encoded,
  onPick,
  testID,
}: {
  centre: MapPin;
  pins?: MapPin[];
  polyline?: string;
  onPick?: (pin: MapPin) => void;
  testID?: string;
}) {
  const host = useRef<HTMLDivElement | null>(null);
  const map = useRef<GLMap | null>(null);
  const markers = useRef<Marker[]>([]);
  // Held in a ref so that changing the handler does not tear the map down.
  const pick = useRef(onPick);
  pick.current = onPick;

  useEffect(() => {
    if (!host.current || map.current || !haveTiles()) return;

    const created = new GLMap({
      container: host.current,
      style: styleFor(tileURL()),
      center: [centre.lon, centre.lat],
      zoom: 13,
      attributionControl: { compact: false },
    });
    created.on('click', (e: MapMouseEvent) =>
      pick.current?.({ lon: e.lngLat.lng, lat: e.lngLat.lat }),
    );
    map.current = created;

    return () => {
      created.remove();
      map.current = null;
    };
    // Created once. The centre below moves it rather than rebuilding it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    map.current?.setCenter([centre.lon, centre.lat]);
  }, [centre.lon, centre.lat]);

  useEffect(() => {
    const m = map.current;
    if (!m) return;

    markers.current.forEach((marker) => marker.remove());
    markers.current = pins.map((p) => new Marker().setLngLat([p.lon, p.lat]).addTo(m));
  }, [pins]);

  useEffect(() => {
    const m = map.current;
    if (!m || !encoded) return;

    const draw = () => {
      // @mapbox/polyline yields [lat, lon]; GeoJSON wants the other order.
      const line = polyline.decode(encoded, 6).map(([lat, lon]) => [lon, lat]);
      const data = {
        type: 'Feature' as const,
        properties: {},
        geometry: { type: 'LineString' as const, coordinates: line },
      };

      const existing = m.getSource('route');
      if (existing) return (existing as GeoJSONSource).setData(data);

      m.addSource('route', { type: 'geojson', data });
      m.addLayer({
        id: 'route',
        type: 'line',
        source: 'route',
        paint: { 'line-width': 4, 'line-color': '#1f6f4a' },
      });
    };

    if (m.isStyleLoaded()) draw();
    else m.once('load', draw);
  }, [encoded]);

  if (!haveTiles()) return <MapFallback testID={testID} />;

  return (
    <View testID={testID} style={styles.box}>
      {/* react-native-web renders a View as a div, so the ref is an element
          maplibre can mount into. */}
      <View ref={host as never} style={styles.canvas} />
    </View>
  );
}

const styles = StyleSheet.create({
  box: { height: 260, borderRadius: 12, overflow: 'hidden' },
  canvas: { flex: 1 },
});

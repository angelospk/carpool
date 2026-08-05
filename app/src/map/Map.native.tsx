// The map, on a phone.
//
// MapLibre rather than react-native-maps, and the reason is iOS. react-native-maps
// draws through the platform's own map — Apple Maps on an iPhone — and its
// mapType="none", the prop that hides that base layer, is Android only. So on
// an iPhone the OSM tiles were being laid over a live Apple Maps view: two maps
// fetched, Apple's showing through wherever a tile had not arrived yet, and
// MapKit used as a substrate for somebody else's tiles, which is not what its
// terms are for.
//
// MapLibre renders the style itself and nothing sits underneath it. It is a
// native module, so it cannot run in Expo Go and needs a development build —
// which this project already makes, because Expo Go dropped Android push
// notifications in SDK 53 and eas.json has carried a development profile since.
//
// The style is the same one the browser draws, out of tiles.ts, so one
// environment variable configures both and neither can drift from the other.

import { StyleSheet, Text, View } from 'react-native';
import {
  Camera,
  GeoJSONSource,
  Layer,
  Map as MapLibre,
  Marker,
  type PressEvent,
} from '@maplibre/maplibre-react-native';
import type { NativeSyntheticEvent } from 'react-native';
import polyline from '@mapbox/polyline';

import { attribution, haveTiles, rasterStyle } from './tiles';
import { MapFallback } from './Fallback';
import type { MapPin } from './Map';

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
  if (!haveTiles()) return <MapFallback testID={testID} />;

  // @mapbox/polyline yields [lat, lon]; GeoJSON wants the other order.
  const line = encoded ? polyline.decode(encoded, 6).map(([lat, lon]) => [lon, lat]) : [];

  return (
    <View testID={testID} style={styles.box}>
      <MapLibre
        style={styles.canvas}
        mapStyle={rasterStyle()}
        onPress={
          onPick
            ? (e: NativeSyntheticEvent<PressEvent>) =>
                onPick({ lon: e.nativeEvent.lngLat[0], lat: e.nativeEvent.lngLat[1] })
            : undefined
        }
      >
        <Camera initialViewState={{ center: [centre.lon, centre.lat], zoom: 13 }} />

        {pins.map((p, i) => (
          <Marker key={`${p.lon},${p.lat},${i}`} lngLat={[p.lon, p.lat]}>
            <View style={styles.pin} />
          </Marker>
        ))}

        {line.length > 1 ? (
          <GeoJSONSource
            id="route"
            data={{
              type: 'Feature',
              properties: {},
              geometry: { type: 'LineString', coordinates: line },
            }}
          >
            <Layer
              id="route-line"
              type="line"
              paint={{ 'line-width': 4, 'line-color': '#1f6f4a' }}
            />
          </GeoJSONSource>
        ) : null}
      </MapLibre>

      {/* Every provider requires their credit on the map, and OSM's licence
          requires crediting the contributors whatever the provider. MapLibre
          draws no attribution control of its own on a phone, so this is it. */}
      <Text style={styles.attribution}>{attribution()}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  box: { height: 260, borderRadius: 12, overflow: 'hidden' },
  canvas: { flex: 1 },
  pin: {
    width: 14,
    height: 14,
    borderRadius: 7,
    backgroundColor: '#1f6f4a',
    borderWidth: 2,
    borderColor: '#fff',
  },
  attribution: {
    position: 'absolute',
    right: 4,
    bottom: 2,
    fontSize: 10,
    color: '#333',
    backgroundColor: 'rgba(255,255,255,0.7)',
    paddingHorizontal: 4,
  },
});

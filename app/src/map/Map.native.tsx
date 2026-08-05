// The map, on a phone.
//
// react-native-maps was already a dependency, and its UrlTile draws exactly the
// XYZ template the web side draws — so the same one environment variable
// configures both, and adding a map costs no new native module and no rebuilt
// development client. What it does mean is that the base map underneath is the
// platform's own; mapType "none" hides it where the platform allows, and where
// it does not the raster tiles sit on top of it.
//
// A vector renderer (@maplibre/maplibre-react-native) would be the better
// answer and is a larger decision: it is a native module, so it cannot run in
// Expo Go and needs a development build. Worth doing when the map is more than
// a picture with pins on it.

import { StyleSheet, View } from 'react-native';
import MapView, { Marker, Polyline, UrlTile } from 'react-native-maps';
import polyline from '@mapbox/polyline';

import { attribution, haveTiles, tileURL } from './tiles';
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

  const line = encoded
    ? polyline.decode(encoded, 6).map(([latitude, longitude]) => ({ latitude, longitude }))
    : [];

  return (
    <View testID={testID} style={styles.box}>
      <MapView
        style={styles.canvas}
        mapType="none"
        initialRegion={{
          latitude: centre.lat,
          longitude: centre.lon,
          latitudeDelta: 0.05,
          longitudeDelta: 0.05,
        }}
        onPress={
          onPick
            ? (e) =>
                onPick({
                  lon: e.nativeEvent.coordinate.longitude,
                  lat: e.nativeEvent.coordinate.latitude,
                })
            : undefined
        }
      >
        <UrlTile urlTemplate={tileURL()} maximumZ={19} flipY={false} />
        {pins.map((p, i) => (
          <Marker key={`${p.lon},${p.lat},${i}`} coordinate={{ latitude: p.lat, longitude: p.lon }} />
        ))}
        {line.length > 1 ? <Polyline coordinates={line} strokeWidth={4} strokeColor="#1f6f4a" /> : null}
      </MapView>
      {/* Every provider requires their credit on the map, and OSM's licence
          requires crediting the contributors whatever the provider. */}
      <Attribution />
    </View>
  );
}

function Attribution() {
  const { Text } = require('react-native');
  return <Text style={styles.attribution}>{attribution()}</Text>;
}

const styles = StyleSheet.create({
  box: { height: 260, borderRadius: 12, overflow: 'hidden' },
  canvas: { flex: 1 },
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

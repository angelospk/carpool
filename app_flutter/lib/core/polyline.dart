import 'geo.dart';

/// The Google encoded-polyline algorithm at precision 6, as OSRM sends it:
/// each point is a latitude then a longitude delta, both in millionths of a
/// degree, zig-zag encoded into base-64 varints.
List<LatLon> decodePolyline6(String s) {
  final points = <LatLon>[];
  var index = 0;
  var lat = 0;
  var lon = 0;

  int? delta() {
    var value = 0;
    var shift = 0;
    while (true) {
      if (index >= s.length) return null;
      final b = s.codeUnitAt(index++) - 63;
      if (b < 0) return null;
      value += (b & 0x1f) << shift;
      shift += 5;
      if (b < 0x20) break;
      if (shift > 60) return null;
    }
    return (value & 1) != 0 ? ~(value >> 1) : (value >> 1);
  }

  while (index < s.length) {
    final dLat = delta();
    if (dLat == null) break;
    final dLon = delta();
    if (dLon == null) break;
    lat += dLat;
    lon += dLon;
    points.add(LatLon(lat / 1000000.0, lon / 1000000.0));
  }
  return points;
}

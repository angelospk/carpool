import 'dart:math' as math;

/// A point on the road, in degrees.
class LatLon {
  const LatLon(this.lat, this.lon);

  final double lat;
  final double lon;

  @override
  bool operator ==(Object other) => other is LatLon && other.lat == lat && other.lon == lon;

  @override
  int get hashCode => Object.hash(lat, lon);

  @override
  String toString() => 'LatLon($lat, $lon)';
}

/// Mean Earth radius, in metres, as OSRM and the server use it.
const double earthRadiusM = 6371008.8;

/// Great-circle distance between two points, by the haversine formula.
double metersBetween(LatLon a, LatLon b) {
  final phi1 = a.lat * math.pi / 180;
  final phi2 = b.lat * math.pi / 180;
  final dPhi = (b.lat - a.lat) * math.pi / 180;
  final dLambda = (b.lon - a.lon) * math.pi / 180;
  final h = math.sin(dPhi / 2) * math.sin(dPhi / 2) +
      math.cos(phi1) * math.cos(phi2) * math.sin(dLambda / 2) * math.sin(dLambda / 2);
  return 2 * earthRadiusM * math.asin(math.min(1, math.sqrt(h)));
}

/// Walking takes a minute per 80 m, rounded up, and never zero.
int walkMinutes(double meters) {
  final minutes = (meters / 80).ceil();
  return minutes < 1 ? 1 : minutes;
}

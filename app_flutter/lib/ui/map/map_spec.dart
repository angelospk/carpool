import 'package:flutter/widgets.dart';

import '../../core/geo.dart';

/// What a dot on the map means.
enum PinKind { car, pickup, dropoff, me, origin, destination }

/// What a road on the map means.
enum LineStyle { route, alternative, walk, detour, live }

/// One dot on the map.
class MapPin {
  const MapPin(this.at, this.kind, {this.label});

  final LatLon at;
  final PinKind kind;
  final String? label;
}

/// One road on the map, in order.
class MapLine {
  const MapLine(this.points, this.style);

  final List<LatLon> points;
  final LineStyle style;
}

/// Everything a screen wants drawn: roads first, dots over them.
class MapSpec {
  const MapSpec({this.lines = const [], this.pins = const []});

  final List<MapLine> lines;
  final List<MapPin> pins;
}

/// A map of this spec, built by the screen that shows it.
typedef MapBuilder = Widget Function(BuildContext context, MapSpec spec);

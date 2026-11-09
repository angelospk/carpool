import 'package:flutter/material.dart' hide Ink;
import 'package:maplibre_gl/maplibre_gl.dart';

import '../theme.dart';
import 'map_spec.dart';

/// The real map: OpenFreeMap's Positron, roads and dots as the spec says.
Widget carpoolMap(BuildContext context, MapSpec spec) => CarpoolMap(spec: spec);

/// The map widget behind [carpoolMap].
class CarpoolMap extends StatefulWidget {
  const CarpoolMap({super.key, required this.spec});

  final MapSpec spec;

  @override
  State<CarpoolMap> createState() => _CarpoolMapState();
}

class _CarpoolMapState extends State<CarpoolMap> {
  static const String _styleUrl = 'https://tiles.openfreemap.org/styles/positron';
  static const String _linesSource = 'carpool-lines';
  static const String _pinsSource = 'carpool-pins';

  MapLibreMapController? _controller;
  bool _styleReady = false;
  String _lastFit = '';

  @override
  void initState() {
    super.initState();
    _lastFit = _fitKey(widget.spec);
  }

  @override
  void didUpdateWidget(CarpoolMap oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (_styleReady) _apply(widget.spec);
  }

  @override
  Widget build(BuildContext context) {
    return MapLibreMap(
      styleString: _styleUrl,
      initialCameraPosition: const CameraPosition(target: LatLng(40.85, 25.9), zoom: 9),
      onMapCreated: (controller) => _controller = controller,
      onStyleLoadedCallback: _onStyleLoaded,
      myLocationEnabled: false,
      myLocationTrackingMode: MyLocationTrackingMode.none,
      rotateGesturesEnabled: false,
      tiltGesturesEnabled: false,
      logoEnabled: false,
      attributionEnabled: true,
    );
  }

  Future<void> _onStyleLoaded() async {
    _styleReady = true;
    _lastFit = '';
    await _install();
    await _apply(widget.spec);
  }

  /// Adds the two sources and one layer per style, ignoring a repeat load.
  Future<void> _install() async {
    final controller = _controller;
    if (controller == null) return;

    for (final layer in _lineLayers.keys) {
      try {
        await controller.removeLayer(layer);
      } catch (_) {
        // A fresh style has none of them yet.
      }
    }
    for (final layer in _pinLayers.keys) {
      try {
        await controller.removeLayer(layer);
      } catch (_) {}
    }
    for (final source in const [_linesSource, _pinsSource]) {
      try {
        await controller.removeSource(source);
      } catch (_) {}
    }

    try {
      await controller.addGeoJsonSource(_linesSource, _empty);
      await controller.addGeoJsonSource(_pinsSource, _empty);
    } catch (_) {
      return;
    }

    for (final entry in _lineLayers.entries) {
      try {
        await controller.addLineLayer(
          _linesSource,
          entry.key,
          entry.value,
          filter: ['==', ['get', 'style'], entry.key],
          enableInteraction: false,
        );
      } catch (_) {}
    }
    for (final entry in _pinLayers.entries) {
      try {
        await controller.addCircleLayer(
          _pinsSource,
          entry.key,
          entry.value,
          filter: ['==', ['get', 'kind'], entry.key],
          enableInteraction: false,
        );
      } catch (_) {}
    }
  }

  /// Replaces the data of both sources with this spec, and fits the camera.
  Future<void> _apply(MapSpec spec) async {
    final controller = _controller;
    if (controller == null || !_styleReady) return;

    try {
      await controller.setGeoJsonSource(_linesSource, _linesGeoJson(spec));
      await controller.setGeoJsonSource(_pinsSource, _pinsGeoJson(spec));
    } catch (_) {
      // The style went away under us; the next load installs again.
      return;
    }

    final fit = _fitKey(spec);
    if (fit != _lastFit && fit.isNotEmpty) {
      _lastFit = fit;
      await _fitCamera(spec);
    }
  }

  Future<void> _fitCamera(MapSpec spec) async {
    final controller = _controller;
    if (controller == null) return;
    final points = [
      ...spec.lines.expand((line) => line.points),
      // The car moves every few seconds; following it would yank the camera
      // away from whatever the person panned to.
      ...spec.pins.where((pin) => pin.kind != PinKind.car).map((pin) => pin.at),
    ];
    if (points.isEmpty) return;
    // The sheet covers the lower half or so of every screen that has a map.
    final bottom = mounted ? MediaQuery.sizeOf(context).height * 0.55 : 340.0;

    var minLat = points.first.lat;
    var maxLat = points.first.lat;
    var minLon = points.first.lon;
    var maxLon = points.first.lon;
    for (final point in points) {
      if (point.lat < minLat) minLat = point.lat;
      if (point.lat > maxLat) maxLat = point.lat;
      if (point.lon < minLon) minLon = point.lon;
      if (point.lon > maxLon) maxLon = point.lon;
    }

    try {
      if (maxLat - minLat < 0.0001 && maxLon - minLon < 0.0001) {
        await controller.animateCamera(
          CameraUpdate.newLatLngZoom(LatLng(minLat, minLon), 14),
          duration: const Duration(milliseconds: 300),
        );
        return;
      }
      final bounds = LatLngBounds(
        southwest: LatLng(minLat, minLon),
        northeast: LatLng(maxLat, maxLon),
      );
      await controller.animateCamera(
        CameraUpdate.newLatLngBounds(bounds, left: 40, top: 80, right: 40, bottom: bottom),
        duration: const Duration(milliseconds: 300),
      );
    } catch (_) {
      // Fitting is a courtesy; a map that shows the wrong place still works.
    }
  }

  String _fitKey(MapSpec spec) {
    final points = [
      ...spec.lines.expand((line) => line.points),
      // The car moves every few seconds; following it would yank the camera
      // away from whatever the person panned to.
      ...spec.pins.where((pin) => pin.kind != PinKind.car).map((pin) => pin.at),
    ];
    if (points.isEmpty) return '';
    return points.map((p) => '${p.lat.toStringAsFixed(3)},${p.lon.toStringAsFixed(3)}').join('|');
  }

  static const Map<String, dynamic> _empty = {
    'type': 'FeatureCollection',
    'features': <dynamic>[],
  };

  Map<String, dynamic> _linesGeoJson(MapSpec spec) => {
        'type': 'FeatureCollection',
        'features': [
          for (final line in spec.lines)
            if (line.points.length >= 2)
              {
                'type': 'Feature',
                'properties': {'style': line.style.name},
                'geometry': {
                  'type': 'LineString',
                  'coordinates': [
                    for (final point in line.points) [point.lon, point.lat],
                  ],
                },
              },
        ],
      };

  Map<String, dynamic> _pinsGeoJson(MapSpec spec) => {
        'type': 'FeatureCollection',
        'features': [
          for (final pin in spec.pins)
            {
              'type': 'Feature',
              'properties': {'kind': pin.kind.name},
              'geometry': {
                'type': 'Point',
                'coordinates': [pin.at.lon, pin.at.lat],
              },
            },
        ],
      };

  static String _hex(Color color) {
    final value = color.toARGB32();
    final r = ((value >> 16) & 0xFF).toRadixString(16).padLeft(2, '0');
    final g = ((value >> 8) & 0xFF).toRadixString(16).padLeft(2, '0');
    final b = (value & 0xFF).toRadixString(16).padLeft(2, '0');
    return '#$r$g$b';
  }

  static final Map<String, LineLayerProperties> _lineLayers = {
    LineStyle.route.name: LineLayerProperties(
      lineColor: _hex(Ink.accent),
      lineWidth: 5,
      lineCap: 'round',
      lineJoin: 'round',
    ),
    LineStyle.alternative.name: LineLayerProperties(
      lineColor: _hex(Ink.alternative),
      lineWidth: 4,
      lineCap: 'round',
      lineJoin: 'round',
    ),
    LineStyle.walk.name: LineLayerProperties(
      lineColor: _hex(Ink.ink),
      lineWidth: 3,
      lineCap: 'round',
      lineDasharray: [1, 2],
    ),
    LineStyle.detour.name: LineLayerProperties(
      lineColor: _hex(Ink.accent),
      lineWidth: 3,
      lineCap: 'round',
      lineDasharray: [1, 2],
    ),
    LineStyle.live.name: LineLayerProperties(
      lineColor: _hex(Ink.accent),
      lineWidth: 2.5,
      lineCap: 'round',
      lineJoin: 'round',
      lineOpacity: 0.7,
    ),
  };

  static final Map<String, CircleLayerProperties> _pinLayers = {
    PinKind.car.name: CircleLayerProperties(
      circleRadius: 9,
      circleColor: _hex(Ink.ink),
      circleStrokeWidth: 2,
      circleStrokeColor: '#ffffff',
    ),
    PinKind.pickup.name: CircleLayerProperties(
      circleRadius: 8,
      circleColor: _hex(Ink.accent),
      circleStrokeWidth: 2,
      circleStrokeColor: '#ffffff',
    ),
    PinKind.dropoff.name: CircleLayerProperties(
      circleRadius: 6,
      circleColor: _hex(Ink.ink),
    ),
    PinKind.destination.name: CircleLayerProperties(
      circleRadius: 6,
      circleColor: _hex(Ink.ink),
    ),
    PinKind.me.name: CircleLayerProperties(
      circleRadius: 6,
      circleColor: _hex(Ink.me),
      circleStrokeWidth: 2,
      circleStrokeColor: '#ffffff',
    ),
    PinKind.origin.name: CircleLayerProperties(
      circleRadius: 6,
      circleColor: '#ffffff',
      circleStrokeWidth: 2,
      circleStrokeColor: _hex(Ink.ink),
    ),
  };
}

import 'dart:async';

import 'package:flutter/material.dart' hide Ink;

import '../core/format.dart';
import '../data/api.dart';
import '../data/models.dart';
import 'driver_setup_screen.dart';
import 'map/carpool_map.dart';
import 'map/map_spec.dart';
import 'messages.dart';
import 'theme.dart';

/// Offer seats: two places, the road to pick, and when.
class PublishScreen extends StatefulWidget {
  const PublishScreen({
    super.key,
    required this.api,
    this.mapBuilder = carpoolMap,
    this.today,
    this.initialOrigin,
    this.initialDestination,
    this.onPublished,
  });

  final Api api;
  final MapBuilder mapBuilder;
  final DateTime? today;
  final Place? initialOrigin;
  final Place? initialDestination;
  final void Function(PublishResult)? onPublished;

  @override
  State<PublishScreen> createState() => _PublishScreenState();
}

class _PublishScreenState extends State<PublishScreen> {
  Place? _origin;
  Place? _destination;
  List<RouteOption> _routes = const [];
  int _selected = 0;
  bool _previewing = false;

  bool _weekly = false;
  DateTime? _oneoffDate;
  final Set<int> _weekdays = {DateTime.now().weekday};

  TimeOfDay _from = const TimeOfDay(hour: 7, minute: 30);
  TimeOfDay _to = const TimeOfDay(hour: 7, minute: 45);
  int _seats = 3;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _origin = widget.initialOrigin;
    _destination = widget.initialDestination;
    final today = widget.today ?? DateTime.now();
    _oneoffDate = DateTime(today.year, today.month, today.day).add(const Duration(days: 1));
    if (_origin != null && _destination != null) {
      WidgetsBinding.instance.addPostFrameCallback((_) => _preview());
    }
  }

  /// Which preview is current. An answer for ends the driver has since
  /// changed would show, and publish, a road between the old ends.
  int _previewGeneration = 0;

  Future<void> _preview() async {
    final origin = _origin;
    final destination = _destination;
    if (origin == null || destination == null) return;
    final generation = ++_previewGeneration;
    setState(() {
      _previewing = true;
      _routes = const [];
      _selected = 0;
    });
    try {
      final routes = await widget.api.routePreview(origin.point, destination.point);
      if (!mounted || generation != _previewGeneration) return;
      setState(() {
        _routes = routes;
        _previewing = false;
      });
    } on ApiException catch (e) {
      if (!mounted || generation != _previewGeneration) return;
      setState(() => _previewing = false);
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(failureText(e))));
    } catch (_) {
      if (!mounted || generation != _previewGeneration) return;
      setState(() => _previewing = false);
    }
  }

  MapSpec _spec() {
    final lines = <MapLine>[];
    final pins = <MapPin>[];
    for (var i = 0; i < _routes.length; i++) {
      final route = _routes[i];
      if (route.line.length < 2) continue;
      lines.add(MapLine(
        route.line,
        i == _selected ? LineStyle.route : LineStyle.alternative,
      ));
    }
    if (_origin != null) pins.add(MapPin(_origin!.point, PinKind.origin));
    if (_destination != null) pins.add(MapPin(_destination!.point, PinKind.destination));
    return MapSpec(lines: lines, pins: pins);
  }

  Future<void> _pickPlace({required bool origin}) async {
    final place = await showDialog<Place>(
      context: context,
      builder: (context) => _PlaceDialog(api: widget.api, title: origin ? 'Από πού;' : 'Πού;'),
    );
    if (place == null) return;
    setState(() {
      if (origin) {
        _origin = place;
      } else {
        _destination = place;
      }
    });
    if (_origin != null && _destination != null) await _preview();
  }

  String get _oneoffText {
    final date = _oneoffDate ?? DateTime.now();
    final month = date.month.toString().padLeft(2, '0');
    final day = date.day.toString().padLeft(2, '0');
    return '${date.year}-$month-$day';
  }

  String _hm(TimeOfDay time) =>
      '${time.hour.toString().padLeft(2, '0')}:${time.minute.toString().padLeft(2, '0')}';

  Future<void> _pickDate() async {
    final now = widget.today ?? DateTime.now();
    final picked = await showDatePicker(
      context: context,
      initialDate: _oneoffDate ?? now,
      firstDate: DateTime(now.year, now.month, now.day),
      lastDate: DateTime(now.year + 1, now.month, now.day),
    );
    if (picked != null) setState(() => _oneoffDate = picked);
  }

  Future<void> _pickTime({required bool from}) async {
    final picked = await showTimePicker(
      context: context,
      initialTime: from ? _from : _to,
    );
    if (picked == null) return;
    setState(() {
      if (from) {
        _from = picked;
      } else {
        _to = picked;
      }
    });
  }

  Future<void> _publish() async {
    final origin = _origin;
    final destination = _destination;
    if (origin == null || destination == null || _busy) {
      if (origin == null || destination == null) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Διάλεξε αφετηρία και προορισμό')),
        );
      }
      return;
    }
    final selected = _routes.isEmpty ? null : _routes[_selected];
    final draft = RouteDraft(
      origin: origin,
      destination: destination,
      via: selected?.via,
      seats: _seats,
      oneoffDate: _weekly ? null : _oneoffText,
      weekdays: _weekly ? (_weekdays.toList()..sort()) : null,
      departEarliest: _hm(_from),
      departLatest: _hm(_to),
    );

    setState(() => _busy = true);
    try {
      final result = await widget.api.publishRoute(draft);
      if (!mounted) return;
      setState(() => _busy = false);
      ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Δημοσιεύτηκε')));
      widget.onPublished?.call(result);
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() => _busy = false);
      if (e.code == 'no_driver_profile' || e.code == 'no_vehicle') {
        await _askForCar();
      } else {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(failureText(e))));
      }
    } catch (_) {
      if (!mounted) return;
      setState(() => _busy = false);
      ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Κάτι πήγε στραβά')));
    }
  }

  Future<void> _askForCar() async {
    final go = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Πρόσθεσε πρώτα το αυτοκίνητό σου'),
        content: const Text('Χρειάζεται μάρκα, χρώμα και πινακίδα για να προσφέρεις θέσεις.'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Όχι τώρα'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Πάμε'),
          ),
        ],
      ),
    );
    if (go != true || !mounted) return;
    await Navigator.push<void>(
      context,
      MaterialPageRoute(builder: (context) => DriverSetupScreen(api: widget.api)),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Stack(
        children: [
          Positioned.fill(child: widget.mapBuilder(context, _spec())),
          Positioned(
            top: 0,
            left: 0,
            child: SafeArea(
              child: Padding(
                padding: const EdgeInsets.all(8),
                child: Material(
                  color: Colors.white,
                  shape: const CircleBorder(),
                  child: IconButton(
                    icon: const Icon(Icons.arrow_back, color: Ink.ink),
                    onPressed: () => Navigator.maybePop(context),
                  ),
                ),
              ),
            ),
          ),
          DraggableScrollableSheet(
            initialChildSize: 0.55,
            minChildSize: 0.3,
            maxChildSize: 0.9,
            builder: (context, controller) => Container(
              decoration: const BoxDecoration(
                color: Colors.white,
                borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
                boxShadow: [
                  BoxShadow(color: Color(0x22000000), blurRadius: 16, offset: Offset(0, -2)),
                ],
              ),
              child: ListView(
                controller: controller,
                padding: const EdgeInsets.fromLTRB(20, 10, 20, 28),
                children: [
                  Center(
                    child: Container(
                      width: 36,
                      height: 4,
                      margin: const EdgeInsets.only(bottom: 16),
                      decoration: BoxDecoration(
                        color: Ink.line,
                        borderRadius: BorderRadius.circular(2),
                      ),
                    ),
                  ),
                  const Text(
                    'Προσφέρω θέσεις',
                    style: TextStyle(color: Ink.ink, fontSize: 22, fontWeight: FontWeight.w700),
                  ),
                  const SizedBox(height: 14),
                  _placeRow('Από', _origin?.name, () => _pickPlace(origin: true)),
                  const SizedBox(height: 8),
                  _placeRow('Πού', _destination?.name, () => _pickPlace(origin: false)),
                  const SizedBox(height: 16),
                  ..._routeRows(),
                  const SizedBox(height: 18),
                  SegmentedButton<bool>(
                    segments: const [
                      ButtonSegment(value: false, label: Text('Μία φορά')),
                      ButtonSegment(value: true, label: Text('Κάθε εβδομάδα')),
                    ],
                    selected: {_weekly},
                    onSelectionChanged: (values) => setState(() => _weekly = values.first),
                  ),
                  const SizedBox(height: 14),
                  if (_weekly) ..._weekdayChips() else _dateRow(),
                  const SizedBox(height: 8),
                  _timeRow('Από', _from, () => _pickTime(from: true)),
                  _timeRow('Έως', _to, () => _pickTime(from: false)),
                  const SizedBox(height: 8),
                  _seatsRow(),
                  const SizedBox(height: 22),
                  FilledButton(
                    onPressed: _busy ? null : _publish,
                    child: const Text('Δημοσίευση'),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _placeRow(String label, String? value, VoidCallback onTap) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(12),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
        decoration: BoxDecoration(
          border: Border.all(color: Ink.line),
          borderRadius: BorderRadius.circular(12),
        ),
        child: Row(
          children: [
            SizedBox(
              width: 48,
              child: Text(label, style: const TextStyle(color: Ink.muted, fontSize: 14)),
            ),
            Expanded(
              child: Text(
                value ?? 'Διάλεξε τόπο',
                style: TextStyle(
                  color: value == null ? Ink.muted : Ink.ink,
                  fontSize: 16,
                  fontWeight: value == null ? FontWeight.w400 : FontWeight.w600,
                ),
              ),
            ),
            const Icon(Icons.search, size: 20, color: Ink.muted),
          ],
        ),
      ),
    );
  }

  List<Widget> _routeRows() {
    if (_previewing) {
      return const [
        Padding(
          padding: EdgeInsets.symmetric(vertical: 12),
          child: Center(child: SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))),
        ),
      ];
    }
    if (_routes.isEmpty) {
      if (_origin != null && _destination != null) {
        return [
          InkWell(
            onTap: _preview,
            child: const Padding(
              padding: EdgeInsets.symmetric(vertical: 8),
              child: Text('Δεν βρέθηκαν δρόμοι. Ξανά;', style: TextStyle(color: Ink.muted)),
            ),
          ),
        ];
      }
      return const [];
    }
    return [
      for (var i = 0; i < _routes.length; i++)
        Padding(
          padding: const EdgeInsets.only(bottom: 8),
          child: _routeRow(i),
        ),
    ];
  }

  Widget _routeRow(int index) {
    final route = _routes[index];
    final selected = index == _selected;
    return InkWell(
      onTap: () => setState(() => _selected = index),
      borderRadius: BorderRadius.circular(12),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
        decoration: BoxDecoration(
          color: Colors.white,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(
            color: selected ? Ink.accent : Ink.line,
            width: selected ? 2 : 1,
          ),
        ),
        child: Row(
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '${route.minutes}$prime · ${kmText(route.km)}',
                    style: const TextStyle(
                      color: Ink.ink,
                      fontSize: 16,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  const SizedBox(height: 2),
                  Text(
                    index == 0 ? 'Ο συντομότερος' : 'Εναλλακτική $index',
                    style: const TextStyle(color: Ink.muted, fontSize: 13),
                  ),
                ],
              ),
            ),
            if (selected) const Icon(Icons.check, color: Ink.accent, size: 20),
          ],
        ),
      ),
    );
  }

  List<Widget> _weekdayChips() {
    const names = ['Δ', 'Τ', 'Τ', 'Π', 'Π', 'Σ', 'Κ'];
    return [
      Wrap(
        spacing: 6,
        children: [
          for (var day = 1; day <= 7; day++)
            FilterChip(
              label: Text(names[day - 1]),
              selected: _weekdays.contains(day),
              onSelected: (on) => setState(() {
                if (on) {
                  _weekdays.add(day);
                } else {
                  _weekdays.remove(day);
                }
              }),
            ),
        ],
      ),
    ];
  }

  Widget _dateRow() {
    return InkWell(
      onTap: _pickDate,
      borderRadius: BorderRadius.circular(12),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
        decoration: BoxDecoration(
          border: Border.all(color: Ink.line),
          borderRadius: BorderRadius.circular(12),
        ),
        child: Row(
          children: [
            const SizedBox(
              width: 60,
              child: Text('Ημέρα', style: TextStyle(color: Ink.muted, fontSize: 14)),
            ),
            Expanded(
              child: Text(
                _oneoffText,
                style: const TextStyle(color: Ink.ink, fontSize: 16, fontWeight: FontWeight.w600),
              ),
            ),
            const Icon(Icons.calendar_today, size: 18, color: Ink.muted),
          ],
        ),
      ),
    );
  }

  Widget _timeRow(String label, TimeOfDay time, VoidCallback onTap) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(12),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
        child: Row(
          children: [
            SizedBox(
              width: 60,
              child: Text(label, style: const TextStyle(color: Ink.muted, fontSize: 14)),
            ),
            Expanded(
              child: Text(
                _hm(time),
                style: const TextStyle(color: Ink.ink, fontSize: 16, fontWeight: FontWeight.w600),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _seatsRow() {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 14),
      child: Row(
        children: [
          const SizedBox(
            width: 60,
            child: Text('Θέσεις', style: TextStyle(color: Ink.muted, fontSize: 14)),
          ),
          Expanded(
            child: Text(
              '$_seats',
              style: const TextStyle(color: Ink.ink, fontSize: 16, fontWeight: FontWeight.w600),
            ),
          ),
          IconButton(
            onPressed: _seats > 1 ? () => setState(() => _seats--) : null,
            icon: const Icon(Icons.remove_circle_outline),
            color: Ink.ink,
          ),
          IconButton(
            onPressed: _seats < 8 ? () => setState(() => _seats++) : null,
            icon: const Icon(Icons.add_circle_outline),
            color: Ink.ink,
          ),
        ],
      ),
    );
  }
}

/// A small search over the server's places.
class _PlaceDialog extends StatefulWidget {
  const _PlaceDialog({required this.api, required this.title});

  final Api api;
  final String title;

  @override
  State<_PlaceDialog> createState() => _PlaceDialogState();
}

class _PlaceDialogState extends State<_PlaceDialog> {
  final TextEditingController _controller = TextEditingController();
  List<Place> _results = const [];
  bool _searching = false;
  String? _error;
  Timer? _debounce;

  @override
  void dispose() {
    _debounce?.cancel();
    _controller.dispose();
    super.dispose();
  }

  void _onChanged(String value) {
    _debounce?.cancel();
    final query = value.trim();
    if (query.length < 2) {
      setState(() => _results = const []);
      return;
    }
    _debounce = Timer(const Duration(milliseconds: 250), () => _search(query));
  }

  Future<void> _search(String query) async {
    setState(() {
      _searching = true;
      _error = null;
    });
    try {
      final places = await widget.api.places(query);
      if (!mounted) return;
      setState(() {
        _results = places;
        _searching = false;
      });
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _error = failureText(e);
        _searching = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _error = 'Κάτι πήγε στραβά';
        _searching = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text(widget.title),
      content: SizedBox(
        width: 360,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: _controller,
              autofocus: true,
              onChanged: _onChanged,
              decoration: const InputDecoration(hintText: 'Χωριό, συνοικία, διεύθυνση'),
            ),
            const SizedBox(height: 8),
            if (_searching)
              const Padding(
                padding: EdgeInsets.all(12),
                child: SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2)),
              ),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.all(12),
                child: Text(_error!, style: const TextStyle(color: Ink.muted)),
              ),
            Flexible(
              child: ListView(
                shrinkWrap: true,
                children: [
                  for (final place in _results)
                    ListTile(
                      dense: true,
                      title: Text(place.name),
                      subtitle: place.area.isEmpty ? null : Text(place.area),
                      onTap: () => Navigator.pop(context, place),
                    ),
                ],
              ),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('Άκυρο'),
        ),
      ],
    );
  }
}

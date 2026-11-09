import 'package:flutter/material.dart' hide Ink;

import '../core/athens_time.dart';
import '../core/format.dart';
import '../data/api.dart';
import '../data/models.dart';
import 'map/carpool_map.dart';
import 'map/map_spec.dart';
import 'theme.dart';

/// One passenger asking for a seat: the detour first, then yes or no.
class AskScreen extends StatefulWidget {
  const AskScreen({
    super.key,
    required this.api,
    required this.askId,
    this.mapBuilder = carpoolMap,
  });

  final Api api;
  final String askId;
  final MapBuilder mapBuilder;

  @override
  State<AskScreen> createState() => _AskScreenState();
}

class _AskScreenState extends State<AskScreen> {
  PendingAsk? _ask;
  TripDetail? _trip;
  bool _loading = true;
  String? _error;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final ask = await widget.api.ask(widget.askId);
      TripDetail? trip;
      if (ask.occurrenceId.isNotEmpty) {
        try {
          trip = await widget.api.trip(ask.occurrenceId);
        } on ApiException {
          trip = null;
        }
      }
      if (!mounted) return;
      setState(() {
        _ask = ask;
        _trip = trip;
        _loading = false;
      });
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.message;
        _loading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _error = 'Κάτι πήγε στραβά';
        _loading = false;
      });
    }
  }

  MapSpec _spec(PendingAsk ask, TripDetail? trip) {
    final lines = <MapLine>[];
    final pins = <MapPin>[];
    if (trip != null && trip.route.length >= 2) {
      lines.add(MapLine(trip.route, LineStyle.route));
    }
    final pickup = trip?.myPickup;
    if (pickup?.point != null) {
      pins.add(MapPin(pickup!.point!, PinKind.pickup));
    } else {
      final first = trip?.stops.where((s) => s.kind == 'pickup' && s.point != null).firstOrNull;
      if (first?.point != null) pins.add(MapPin(first!.point!, PinKind.pickup));
    }
    return MapSpec(lines: lines, pins: pins);
  }

  Future<void> _decline(PendingAsk ask) async {
    if (_busy) return;
    setState(() => _busy = true);
    try {
      await widget.api.declineAsk(ask.id);
      if (!mounted) return;
      Navigator.maybePop(context);
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() => _busy = false);
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(e.message)));
    }
  }

  Future<void> _accept(PendingAsk ask) async {
    if (_busy) return;
    setState(() => _busy = true);
    try {
      await widget.api.acceptAsk(ask.id);
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Κλείστηκε')));
      Navigator.maybePop(context);
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() => _busy = false);
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(e.message)));
    } catch (_) {
      if (!mounted) return;
      setState(() => _busy = false);
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Κάτι πήγε στραβά')),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        bottom: false,
        child: Column(
          children: [
            Expanded(child: _body()),
          ],
        ),
      ),
    );
  }

  Widget _body() {
    final ask = _ask;
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (ask == null) {
      return _errorPanel(_error ?? 'Δεν βρέθηκε η ερώτηση');
    }
    final height = MediaQuery.sizeOf(context).height;
    return Column(
      children: [
        SizedBox(
          height: height * 0.4,
          width: double.infinity,
          child: widget.mapBuilder(context, _spec(ask, _trip)),
        ),
        Expanded(child: _panel(ask)),
      ],
    );
  }

  Widget _errorPanel(String message) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(message, textAlign: TextAlign.center, style: const TextStyle(color: Ink.muted)),
            const SizedBox(height: 16),
            OutlinedButton(onPressed: _load, child: const Text('Ξανά')),
          ],
        ),
      ),
    );
  }

  Widget _panel(PendingAsk ask) {
    final rating = ask.passengerRating;
    final ratingText = rating == null
        ? ''
        : ' · ★ ${rating.toStringAsFixed(1).replaceAll('.', ',')} (${ask.passengerRatingCount})';
    return Container(
      color: Colors.white,
      child: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(20, 18, 20, 20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              '${ask.passengerName} ζητά θέση$ratingText',
              style: const TextStyle(color: Ink.muted, fontSize: 14),
            ),
            const SizedBox(height: 10),
            Text(
              detourMinutes(ask.detourMin),
              style: const TextStyle(
                color: Ink.ink,
                fontSize: 44,
                fontWeight: FontWeight.w700,
                height: 1.1,
              ),
            ),
            const SizedBox(height: 2),
            Text(
              'παράκαμψη · +${kmTextNumber(ask.detourKm)} χλμ',
              style: const TextStyle(color: Ink.muted, fontSize: 15),
            ),
            const SizedBox(height: 16),
            const Divider(height: 1),
            const SizedBox(height: 14),
            _row('Παραλαβή', ask.originLabel),
            _row('Παράδοση', ask.destinationLabel),
            _row('Συνεισφορά', '${eur(ask.priceEur)} / θέση'),
            _row('Ώρα', '${athensHm(ask.windowStart)}–${athensHm(ask.windowEnd)}'),
            const SizedBox(height: 6),
            Text(
              'Λήγει στις ${athensHm(ask.expiresAt)}',
              style: const TextStyle(color: Ink.muted, fontSize: 13),
            ),
            const SizedBox(height: 20),
            Row(
              children: [
                Expanded(
                  child: OutlinedButton(
                    onPressed: _busy ? null : () => _decline(ask),
                    child: const Text('Όχι'),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  flex: 2,
                  child: FilledButton(
                    onPressed: _busy ? null : () => _accept(ask),
                    child: const Text('Ναι, πάρ\' την'),
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _row(String label, String value) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 5),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 104,
            child: Text(label, style: const TextStyle(color: Ink.muted, fontSize: 14)),
          ),
          Expanded(
            child: Text(
              value,
              style: const TextStyle(color: Ink.ink, fontSize: 15, fontWeight: FontWeight.w500),
            ),
          ),
        ],
      ),
    );
  }
}

import 'package:flutter/material.dart' hide Ink;

import '../core/athens_time.dart';
import '../core/format.dart';
import '../core/geo.dart';
import '../data/api.dart';
import '../data/models.dart';
import 'map/carpool_map.dart';
import 'map/map_spec.dart';
import 'theme.dart';

/// What the person opens the app to: the cars out now, and their own day.
class HomeScreen extends StatefulWidget {
  const HomeScreen({
    super.key,
    required this.api,
    this.mapBuilder = carpoolMap,
    this.onOpenTrip,
    this.onOpenAsk,
    this.onOpenRequest,
    this.onSearch,
    this.onPublish,
    this.onProfile,
    this.routeObserver,
  });

  final Api api;
  final MapBuilder mapBuilder;
  final void Function(String id)? onOpenTrip;
  final void Function(String id)? onOpenAsk;
  final void Function(String id)? onOpenRequest;
  final VoidCallback? onSearch;
  final VoidCallback? onPublish;
  final VoidCallback? onProfile;

  /// Tells the screen when a pushed screen closes, so the lists read again.
  final RouteObserver<ModalRoute<void>>? routeObserver;

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> with RouteAware {
  Me? _me;
  List<TripSummary> _trips = const [];
  List<PendingAsk> _asks = const [];
  List<OpenRequest> _requests = const [];
  List<({TripSummary trip, String driverName})> _past = const [];
  List<List<LatLon>> _live = const [];
  bool _loading = true;
  bool _subscribed = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_subscribed) return;
    final observer = widget.routeObserver;
    if (observer == null) return;
    final route = ModalRoute.of<void>(context);
    if (route == null) return;
    observer.subscribe(this, route);
    _subscribed = true;
  }

  @override
  void dispose() {
    if (_subscribed) widget.routeObserver?.unsubscribe(this);
    super.dispose();
  }

  /// A screen on top of this one closed: what it changed may be here too.
  @override
  void didPopNext() {
    _load();
  }

  Future<void> _load() async {
    try {
      final results = await Future.wait<Object?>([
        _soft(widget.api.me()),
        _soft(widget.api.upcomingTrips()),
        _soft(widget.api.pendingAsks()),
        _soft(widget.api.myRequests()),
        _soft(widget.api.liveMap()),
        _soft(widget.api.pastTrips()),
      ]);
      if (!mounted) return;
      setState(() {
        _me = results[0] as Me?;
        _trips = (results[1] as List<TripSummary>?) ?? const [];
        _asks = (results[2] as List<PendingAsk>?) ?? const [];
        _requests = (results[3] as List<OpenRequest>?) ?? const [];
        _live = (results[4] as List<List<LatLon>>?) ?? const [];
        _past = (results[5] as List<({TripSummary trip, String driverName})>?) ?? const [];
        _loading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _loading = false);
    }
  }

  /// A call that may fail without taking the screen down with it.
  Future<T?> _soft<T extends Object>(Future<T> call) async {
    try {
      return await call;
    } catch (_) {
      return null;
    }
  }

  MapSpec _spec() {
    final lines = [
      for (final path in _live)
        if (path.length >= 2) MapLine(path, LineStyle.live),
    ];
    return MapSpec(lines: lines);
  }

  String get _greeting {
    final name = _me?.name ?? '';
    final first = name.split(' ').firstWhere((part) => part.isNotEmpty, orElse: () => name);
    final hour = athensNow().hour;
    return hour < 12 ? 'Καλημέρα, $first' : 'Καλησπέρα, $first';
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
            right: 0,
            child: SafeArea(
              bottom: false,
              child: Padding(
                padding: const EdgeInsets.fromLTRB(16, 10, 16, 0),
                child: _searchBar(),
              ),
            ),
          ),
          DraggableScrollableSheet(
            initialChildSize: 0.42,
            minChildSize: 0.28,
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
                children: _sheet(),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _searchBar() {
    return Material(
      color: Colors.white,
      borderRadius: BorderRadius.circular(12),
      child: InkWell(
        onTap: widget.onSearch,
        borderRadius: BorderRadius.circular(12),
        child: Container(
          height: 52,
          padding: const EdgeInsets.symmetric(horizontal: 16),
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(12),
            border: Border.all(color: Ink.line),
          ),
          child: const Row(
            children: [
              Icon(Icons.search, size: 20, color: Ink.muted),
              SizedBox(width: 10),
              Text(
                'Πού πας;',
                style: TextStyle(color: Ink.muted, fontSize: 16, fontWeight: FontWeight.w500),
              ),
            ],
          ),
        ),
      ),
    );
  }

  List<Widget> _sheet() {
    final rows = <Widget>[
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
      Row(
        children: [
          Expanded(
            child: Text(
              _greeting,
              style: const TextStyle(color: Ink.ink, fontSize: 22, fontWeight: FontWeight.w700),
            ),
          ),
          if (widget.onProfile != null)
            IconButton(
              onPressed: widget.onProfile,
              icon: const Icon(Icons.person_outline, color: Ink.muted),
            ),
        ],
      ),
    ];

    if (_asks.isNotEmpty) {
      rows.add(const SizedBox(height: 18));
      rows.add(_sectionTitle('Ερωτήσεις για θέση'));
      for (final ask in _asks) {
        rows.add(_askRow(ask));
      }
    }

    if (_trips.isNotEmpty) {
      rows.add(const SizedBox(height: 18));
      rows.add(_sectionTitle('Επόμενες διαδρομές'));
      for (final trip in _trips) {
        rows.add(_tripCard(trip));
      }
    } else if (!_loading) {
      rows.add(const SizedBox(height: 18));
      rows.add(const Text(
        'Δεν έχεις κάποια διαδρομή ακόμα.',
        style: TextStyle(color: Ink.muted, fontSize: 15),
      ));
    }

    if (_requests.isNotEmpty) {
      rows.add(const SizedBox(height: 18));
      rows.add(_sectionTitle('Τα αιτήματά σου'));
      for (final request in _requests) {
        rows.add(_requestRow(request));
      }
    }

    rows.add(const SizedBox(height: 24));
    rows.add(FilledButton(
      onPressed: widget.onSearch,
      child: const Text('Ψάξε διαδρομή'),
    ));
    rows.add(const SizedBox(height: 10));
    rows.add(OutlinedButton(
      onPressed: widget.onPublish,
      child: const Text('Προσφέρω θέσεις'),
    ));

    if (_past.isNotEmpty) {
      rows.add(const SizedBox(height: 24));
      rows.add(_sectionTitle('Παλιότερες διαδρομές'));
      for (final past in _past) {
        rows.add(_pastRow(past.trip, past.driverName));
      }
    }
    return rows;
  }

  /// A ride already taken: opening it is where the rating is.
  Widget _pastRow(TripSummary trip, String driverName) {
    final status = switch (trip.status) {
      'completed' => ' · Ολοκληρώθηκε',
      'cancelled' => ' · Ακυρώθηκε',
      _ => '',
    };
    return InkWell(
      onTap: widget.onOpenTrip == null ? null : () => widget.onOpenTrip!(trip.id),
      borderRadius: BorderRadius.circular(12),
      child: Container(
        margin: const EdgeInsets.only(bottom: 8),
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
        decoration: BoxDecoration(
          border: Border.all(color: Ink.line),
          borderRadius: BorderRadius.circular(12),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '${trip.originLabel} → ${trip.destinationLabel}',
              style: const TextStyle(color: Ink.ink, fontSize: 16, fontWeight: FontWeight.w600),
            ),
            const SizedBox(height: 2),
            Text(
              '${athensDay(trip.departEarliest)} · $driverName$status',
              style: const TextStyle(color: Ink.muted, fontSize: 14),
            ),
          ],
        ),
      ),
    );
  }

  Widget _sectionTitle(String text) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Text(
        text,
        style: const TextStyle(color: Ink.ink, fontSize: 16, fontWeight: FontWeight.w700),
      ),
    );
  }

  Widget _askRow(PendingAsk ask) {
    return InkWell(
      onTap: widget.onOpenAsk == null ? null : () => widget.onOpenAsk!(ask.id),
      borderRadius: BorderRadius.circular(12),
      child: Container(
        margin: const EdgeInsets.only(bottom: 8),
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
        decoration: BoxDecoration(
          border: Border.all(color: Ink.line),
          borderRadius: BorderRadius.circular(12),
        ),
        child: Row(
          children: [
            Expanded(
              child: Text(
                ask.passengerName,
                style: const TextStyle(color: Ink.ink, fontSize: 16, fontWeight: FontWeight.w600),
              ),
            ),
            Text(
              detourMinutes(ask.detourMin),
              style: const TextStyle(color: Ink.accent, fontSize: 15, fontWeight: FontWeight.w700),
            ),
          ],
        ),
      ),
    );
  }

  Widget _tripCard(TripSummary trip) {
    final role = trip.role == TripRole.driver ? 'οδηγός' : 'επιβάτης';
    return InkWell(
      onTap: widget.onOpenTrip == null ? null : () => widget.onOpenTrip!(trip.id),
      borderRadius: BorderRadius.circular(12),
      child: Container(
        margin: const EdgeInsets.only(bottom: 10),
        padding: const EdgeInsets.fromLTRB(14, 12, 14, 14),
        decoration: BoxDecoration(
          border: Border.all(color: Ink.line),
          borderRadius: BorderRadius.circular(12),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              athensHm(trip.departEarliest),
              style: const TextStyle(color: Ink.ink, fontSize: 26, fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 4),
            Text(
              '${trip.originLabel} → ${trip.destinationLabel}',
              style: const TextStyle(color: Ink.ink, fontSize: 15),
            ),
            const SizedBox(height: 2),
            Text(
              '${athensDay(trip.departEarliest)} · $role',
              style: const TextStyle(color: Ink.muted, fontSize: 13),
            ),
          ],
        ),
      ),
    );
  }

  Widget _requestRow(OpenRequest request) {
    return InkWell(
      onTap: widget.onOpenRequest == null ? null : () => widget.onOpenRequest!(request.id),
      borderRadius: BorderRadius.circular(12),
      child: Container(
        margin: const EdgeInsets.only(bottom: 8),
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
        decoration: BoxDecoration(
          border: Border.all(color: Ink.line),
          borderRadius: BorderRadius.circular(12),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '${request.originLabel} → ${request.destinationLabel}',
              style: const TextStyle(color: Ink.ink, fontSize: 15, fontWeight: FontWeight.w600),
            ),
            const SizedBox(height: 2),
            Text(
              '${athensHm(request.windowStart)}–${athensHm(request.windowEnd)} · ${request.askingNow ? 'Ρωτάμε οδηγούς…' : 'Σε αναμονή'}',
              style: const TextStyle(color: Ink.muted, fontSize: 13),
            ),
          ],
        ),
      ),
    );
  }
}

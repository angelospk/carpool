import 'dart:async';

import 'package:flutter/material.dart' hide Ink;
import 'package:geolocator/geolocator.dart';
import 'package:share_plus/share_plus.dart';
import 'package:url_launcher/url_launcher.dart';

import '../core/athens_time.dart';
import '../core/format.dart';
import '../core/geo.dart';
import '../data/api.dart';
import '../data/models.dart';
import 'map/carpool_map.dart';
import 'map/map_spec.dart';
import 'messages.dart';
import 'theme.dart';
import 'trip_chat.dart';
import 'user_profile_screen.dart';

/// A trip as the person on it sees it: the map, then who, when, and what to do.
class TripScreen extends StatefulWidget {
  const TripScreen({
    super.key,
    required this.api,
    required this.tripId,
    this.mapBuilder = carpoolMap,
    this.locate,
    this.share,
    this.livePoll = const Duration(seconds: 10),
    this.chatPoll = const Duration(seconds: 10),
    this.onStartDrive,
    this.onEndDrive,
  });

  final Api api;
  final String tripId;
  final MapBuilder mapBuilder;

  /// Where the person is now, or null when the phone will not say.
  final Future<LatLon?> Function()? locate;

  /// Shares a text; true when the sheet opened.
  final Future<void> Function(String text)? share;

  final Duration livePoll;

  /// How often the chat looks for new messages.
  final Duration chatPoll;

  /// Starts sharing the car's position: after «Φεύγω», and again when the
  /// screen opens on a drive the server still has running (the app was killed
  /// mid-drive and the tracker with it).
  final Future<void> Function(TripDetail trip)? onStartDrive;

  /// Stops sharing it, after «Τέλος διαδρομής».
  final Future<void> Function(TripDetail trip)? onEndDrive;

  @override
  State<TripScreen> createState() => _TripScreenState();
}

class _TripScreenState extends State<TripScreen> {
  TripDetail? _trip;
  LiveState? _live;
  LatLon? _me;
  bool _loading = true;
  String? _error;
  bool _busy = false;
  bool _confirmed = false;
  bool _ratingBusy = false;
  final Map<String, int> _stars = {};
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  Future<LatLon?> _findMe() async {
    final locate = widget.locate;
    if (locate != null) return locate();
    try {
      final position = await Geolocator.getCurrentPosition();
      return LatLon(position.latitude, position.longitude);
    } catch (_) {
      return null;
    }
  }

  Future<void> _shareText(String text) async {
    final share = widget.share;
    if (share != null) {
      await share(text);
      return;
    }
    await SharePlus.instance.share(ShareParams(text: text));
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final trip = await widget.api.trip(widget.tripId);
      if (!mounted) return;
      setState(() {
        _trip = trip;
        _confirmed = trip.confirmedByMe;
        _loading = false;
      });
      _startPolling();
      if (trip.role == TripRole.driver && trip.driveSetAt != null && trip.driveEndedAt == null) {
        await widget.onStartDrive?.call(trip);
      }
      await _pollLive();
      final me = await _findMe();
      if (!mounted) return;
      if (me != null) setState(() => _me = me);
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _error = failureText(e);
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

  void _startPolling() {
    _timer?.cancel();
    if (widget.livePoll <= Duration.zero) return;
    _timer = Timer.periodic(widget.livePoll, (_) => _pollLive());
  }

  Future<void> _pollLive() async {
    try {
      final live = await widget.api.live(widget.tripId);
      if (!mounted) return;
      setState(() => _live = live);
    } on ApiException catch (e) {
      if (e.status == 409 && e.code == 'not_sharing_now') {
        if (!mounted) return;
        setState(() {
          _live = LiveState(
            opens: _live?.opens ?? DateTime.now().toUtc(),
            closes: _live?.closes ?? DateTime.now().toUtc(),
            positions: const [],
          );
        });
      }
      // Any other refusal leaves what we have; the next poll may work.
    } catch (_) {
      // A missing live call is not a reason to lose the trip.
    }
  }

  LivePosition? get _driverPosition {
    final trip = _trip;
    final live = _live;
    if (trip == null || live == null) return null;
    for (final position in live.positions) {
      if (position.userId == trip.driver.id) return position;
    }
    return null;
  }

  MapSpec _spec(TripDetail trip) {
    final lines = <MapLine>[];
    final pins = <MapPin>[];
    if (trip.route.length >= 2) lines.add(MapLine(trip.route, LineStyle.route));

    final driver = _driverPosition;
    if (driver != null) pins.add(MapPin(driver.point, PinKind.car));

    final myPickup = trip.myPickup;
    LatLon? pickupPoint = myPickup?.point;
    if (pickupPoint != null) pins.add(MapPin(pickupPoint, PinKind.pickup));

    LatLon? dropoffPoint = _myDropoff(trip)?.point;
    if (dropoffPoint == null) {
      for (final passenger in trip.passengers) {
        if (passenger.dropoff != null) {
          dropoffPoint = passenger.dropoff;
          break;
        }
      }
    }
    if (dropoffPoint != null) pins.add(MapPin(dropoffPoint, PinKind.dropoff));

    final me = _me;
    if (me != null) pins.add(MapPin(me, PinKind.me));
    if (me != null && pickupPoint != null) {
      lines.add(MapLine([me, pickupPoint], LineStyle.walk));
    }

    return MapSpec(lines: lines, pins: pins);
  }

  TripStop? _myDropoff(TripDetail trip) {
    for (final stop in trip.stops) {
      if (stop.kind == 'dropoff' && stop.mine) return stop;
    }
    return null;
  }

  void _say(String text) {
    final messenger = ScaffoldMessenger.of(context);
    messenger.hideCurrentSnackBar();
    messenger.showSnackBar(SnackBar(content: Text(text)));
  }

  Future<void> _confirm(TripDetail trip) async {
    if (_busy || _confirmed) return;
    setState(() => _busy = true);
    try {
      await widget.api.confirmTrip(trip.id);
      if (!mounted) return;
      setState(() => _confirmed = true);
      await _refreshTrip();
    } catch (e) {
      if (!mounted) return;
      _say(failureText(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// Whether the trip has not started, so leaving it is still allowed.
  bool _before(TripDetail trip) => trip.status == 'scheduled' && trip.driveSetAt == null;

  Future<void> _openPerson(Participant person) async {
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => UserProfileScreen(api: widget.api, userId: person.id, name: person.name),
      ),
    );
  }

  Future<void> _rate(TripDetail trip, String rateeId) async {
    final stars = _stars[rateeId];
    if (stars == null || _ratingBusy) return;
    setState(() => _ratingBusy = true);
    try {
      await widget.api.rate(trip.id, rateeId: rateeId, stars: stars);
      if (!mounted) return;
      await _refreshTrip();
    } catch (e) {
      if (!mounted) return;
      _say(failureText(e));
    } finally {
      if (mounted) setState(() => _ratingBusy = false);
    }
  }

  /// Leaves the seat: the server frees it and tells the driver.
  Future<void> _leaveTrip(TripDetail trip) async {
    final bookingId = trip.bookingId;
    if (bookingId == null) return;
    final ok = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        content: Text('Η θέση σου επιστρέφει και ο/η ${trip.driver.name} θα ειδοποιηθεί.'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('Άκυρο'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('Δεν θα έρθω'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted || _busy) return;
    setState(() => _busy = true);
    try {
      await widget.api.cancelBooking(bookingId);
      if (!mounted) return;
      Navigator.of(context).pop();
    } on ApiException catch (e) {
      if (!mounted) return;
      _say(e.code == 'not_found' ? 'Η θέση αυτή δεν είναι πια δική σου' : failureText(e));
    } catch (e) {
      if (!mounted) return;
      _say(failureText(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// Calls the whole departure off, and says who will hear about it.
  Future<void> _cancelTrip(TripDetail trip) async {
    final count = trip.passengers.length;
    final ok = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        content: Text(
          count == 0
              ? 'Η διαδρομή αυτή δεν θα γίνει.'
              : count == 1
                  ? 'Η διαδρομή δεν θα γίνει. Θα ειδοποιηθεί 1 επιβάτης.'
                  : 'Η διαδρομή δεν θα γίνει. Θα ειδοποιηθούν $count επιβάτες.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('Άκυρο'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('Ακύρωση διαδρομής'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted || _busy) return;
    setState(() => _busy = true);
    try {
      await widget.api.cancelTrip(trip.id);
      if (!mounted) return;
      _say('Η διαδρομή ακυρώθηκε');
      await _refreshTrip();
    } catch (e) {
      if (!mounted) return;
      _say(failureText(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _shareTrip(TripDetail trip) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      final link = await widget.api.shareTrip(trip.id);
      final car = [trip.driver.carModel, trip.driver.carPlate]
          .whereType<String>()
          .where((part) => part.isNotEmpty)
          .join(' ');
      final text =
          'Είμαι στη διαδρομή ${trip.originLabel} → ${trip.destinationLabel} '
          'με ${trip.driver.name}${car.isEmpty ? '' : ' ($car)'}. '
          'Δες πού είμαστε: ${link.url}';
      await _shareText(text);
    } on ApiException catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(failureText(e))));
    } catch (_) {
      messenger.showSnackBar(const SnackBar(content: Text('Δεν μπόρεσα να μοιραστώ τη διαδρομή')));
    }
  }

  Future<void> _call(TripDetail trip) async {
    final phone = trip.driver.phone;
    if (phone == null) return;
    final uri = Uri(scheme: 'tel', path: phone);
    await launchUrl(uri);
  }

  Future<void> _walkDirections(LatLon point) async {
    final uri = Uri.parse(
      'https://www.google.com/maps/dir/?api=1&destination=${point.lat},${point.lon}&travelmode=walking',
    );
    await launchUrl(uri, mode: LaunchMode.externalApplication);
  }

  Future<void> _startDrive(TripDetail trip) async {
    if (_busy) return;
    setState(() => _busy = true);
    try {
      await widget.api.startDrive(trip.id, DateTime.now().toUtc());
      if (!mounted) return;
      setState(() => _busy = false);
      await widget.onStartDrive?.call(trip);
      if (!mounted) return;
      await _refreshTrip();
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() => _busy = false);
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(failureText(e))));
    } catch (e) {
      if (!mounted) return;
      setState(() => _busy = false);
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(failureText(e))));
    }
  }

  /// Re-reads the trip after a change the server made, so the sheet follows.
  Future<void> _refreshTrip() async {
    try {
      final trip = await widget.api.trip(widget.tripId);
      if (!mounted) return;
      setState(() => _trip = trip);
    } catch (_) {
      // The last state is still true enough; a reload will fix it.
    }
  }

  Future<void> _endDrive(TripDetail trip) async {
    if (_busy) return;
    setState(() => _busy = true);
    try {
      await widget.api.endDrive(trip.id);
      await widget.onEndDrive?.call(trip);
      if (!mounted) return;
      setState(() => _busy = false);
      await _refreshTrip();
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() => _busy = false);
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(failureText(e))));
    } catch (e) {
      if (!mounted) return;
      setState(() => _busy = false);
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(failureText(e))));
    }
  }

  @override
  Widget build(BuildContext context) {
    final trip = _trip;
    return Scaffold(
      body: Stack(
        children: [
          Positioned.fill(
            child: trip == null
                ? Container(color: Ink.surface)
                : widget.mapBuilder(context, _spec(trip)),
          ),
          Positioned(
            top: 0,
            left: 0,
            child: SafeArea(
              child: Padding(
                padding: const EdgeInsets.all(8),
                child: Material(
                  color: Colors.white,
                  shape: const CircleBorder(),
                  elevation: 0,
                  child: IconButton(
                    icon: const Icon(Icons.arrow_back, color: Ink.ink),
                    onPressed: () => Navigator.maybePop(context),
                  ),
                ),
              ),
            ),
          ),
          if (_loading)
            const Positioned.fill(
              child: Center(child: CircularProgressIndicator()),
            ),
          if (!_loading && trip == null)
            Positioned.fill(child: Center(child: _errorBox())),
          if (trip != null)
            DraggableScrollableSheet(
              initialChildSize: 0.45,
              minChildSize: 0.25,
              maxChildSize: 0.9,
              builder: (context, controller) => _sheet(context, controller, trip),
            ),
        ],
      ),
    );
  }

  Widget _errorBox() {
    return Container(
      margin: const EdgeInsets.all(24),
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: Ink.line),
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(_error ?? 'Δεν βρέθηκε η διαδρομή', style: const TextStyle(color: Ink.muted)),
          const SizedBox(height: 14),
          OutlinedButton(onPressed: _load, child: const Text('Ξανά')),
        ],
      ),
    );
  }

  Widget _sheet(BuildContext context, ScrollController controller, TripDetail trip) {
    return Container(
      decoration: const BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
        boxShadow: [BoxShadow(color: Color(0x22000000), blurRadius: 16, offset: Offset(0, -2))],
      ),
      child: ListView(
        controller: controller,
        padding: const EdgeInsets.fromLTRB(20, 10, 20, 28),
        children: [
          Center(
            child: Container(
              width: 36,
              height: 4,
              margin: const EdgeInsets.only(bottom: 18),
              decoration: BoxDecoration(
                color: Ink.line,
                borderRadius: BorderRadius.circular(2),
              ),
            ),
          ),
          if (_cancelled(trip)) ...[
            const SizedBox(height: 4),
            const Text('Ακυρώθηκε', style: TextStyle(color: Ink.muted, fontSize: 15)),
            const SizedBox(height: 5),
          ],
          if (trip.role == TripRole.passenger) ..._passengerBody(trip) else ..._driverBody(trip),
        ],
      ),
    );
  }

  /// Whether the whole departure was called off: then there is nothing to do on it.
  bool _cancelled(TripDetail trip) => trip.status == 'cancelled';

  // ---------------------------------------------------------------- passenger

  List<Widget> _passengerBody(TripDetail trip) {
    final live = _live;
    final driverPosition = _driverPosition;
    final pickup = trip.myPickup;
    final when = pickup?.at ?? trip.pickupAt ?? trip.departEarliest;
    final car = [trip.driver.carModel, trip.driver.carColor]
        .whereType<String>()
        .where((part) => part.isNotEmpty)
        .join(' ');

    return [
      _liveRow(live, driverPosition),
      const SizedBox(height: 10),
      InkWell(
        key: Key('person-${trip.driver.id}'),
        onTap: () => _openPerson(trip.driver),
        child: Text(
          live?.driverEtaMin != null
              ? '${trip.driver.name} · φτάνει ${etaText(live!.driverEtaMin!)}'
              : '${trip.driver.name} · ${athensHm(when)}',
          style: const TextStyle(color: Ink.ink, fontSize: 22, fontWeight: FontWeight.w700),
        ),
      ),
      if (pickup != null) ...[
        const SizedBox(height: 6),
        Text(
          '${pickup.label ?? ''} · ${athensHm(when)}',
          style: const TextStyle(color: Ink.muted, fontSize: 15),
        ),
      ],
      if (_me != null && pickup?.point != null) ...[
        const SizedBox(height: 4),
        Text(
          'Περπάτα ${walkText(metersBetween(_me!, pickup!.point!))}',
          style: const TextStyle(color: Ink.muted, fontSize: 15),
        ),
      ],
      const SizedBox(height: 18),
      _carRow(trip, car, pickup?.point),
      const SizedBox(height: 16),
      Row(
        children: [
          Expanded(
            child: OutlinedButton(
              onPressed: () => _shareTrip(trip),
              child: const Text('Μοιράσου'),
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: OutlinedButton(
              onPressed: trip.driver.phone == null ? null : () => _call(trip),
              child: const Text('Κλήση'),
            ),
          ),
        ],
      ),
      const SizedBox(height: 20),
      _timeline(trip),
      const SizedBox(height: 20),
      if (!_cancelled(trip)) ...[
        _confirmButton(trip),
        ..._ratingSection(trip),
        if (_before(trip) && trip.bookingId != null) ...[
          const SizedBox(height: 12),
          OutlinedButton(
            style: OutlinedButton.styleFrom(foregroundColor: Ink.danger),
            onPressed: _busy ? null : () => _leaveTrip(trip),
            child: const Text('Δεν θα έρθω'),
          ),
        ],
      ],
      const SizedBox(height: 20),
      TripChat(
        key: ValueKey(trip.id),
        api: widget.api,
        tripId: trip.id,
        myId: trip.myId,
        poll: widget.chatPoll,
      ),
    ];
  }

  Widget _liveRow(LiveState? live, LivePosition? position) {
    if (position != null) {
      final age = DateTime.now().toUtc().difference(position.at.toUtc());
      final text = age.inSeconds < 60
          ? 'θέση πριν ${age.inSeconds < 0 ? 0 : age.inSeconds}″'
          : 'θέση πριν ${age.inMinutes}′';
      return Row(
        children: [
          Container(
            width: 8,
            height: 8,
            decoration: const BoxDecoration(color: Ink.accent, shape: BoxShape.circle),
          ),
          const SizedBox(width: 8),
          Text('Ζωντανά', style: const TextStyle(color: Ink.accent, fontSize: 13, fontWeight: FontWeight.w600)),
          const SizedBox(width: 8),
          Text(text, style: const TextStyle(color: Ink.muted, fontSize: 13)),
        ],
      );
    }
    return Text(
      live != null ? 'Ζωντανή θέση από ${athensHm(live.opens)}' : 'Ζωντανή θέση κλειστή',
      style: const TextStyle(color: Ink.muted, fontSize: 13),
    );
  }

  Widget _carRow(TripDetail trip, String car, LatLon? pickupPoint) {
    return Container(
      padding: const EdgeInsets.fromLTRB(14, 12, 6, 12),
      decoration: BoxDecoration(
        color: Ink.surface,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        children: [
          const Icon(Icons.directions_car, color: Ink.ink, size: 22),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (car.isNotEmpty)
                  Text(
                    car,
                    style: const TextStyle(color: Ink.ink, fontSize: 15, fontWeight: FontWeight.w600),
                  ),
                if (trip.driver.carPlate != null)
                  Text(
                    trip.driver.carPlate!,
                    style: const TextStyle(color: Ink.muted, fontSize: 13),
                  ),
              ],
            ),
          ),
          if (pickupPoint != null)
            TextButton(
              onPressed: () => _walkDirections(pickupPoint),
              child: const Text('Οδηγίες'),
            ),
        ],
      ),
    );
  }

  Widget _timeline(TripDetail trip) {
    final pickup = trip.myPickup;
    final rows = <Widget>[
      _timelineRow(athensHm(trip.departEarliest), trip.originLabel, true),
      if (pickup != null)
        _timelineRow(
          athensHm(pickup.at ?? trip.pickupAt ?? trip.departEarliest),
          pickup.label ?? '',
          false,
        ),
      _timelineRow('', trip.destinationLabel, false),
    ];
    return Container(
      padding: const EdgeInsets.fromLTRB(16, 7, 16, 7),
      decoration: BoxDecoration(
        border: Border.all(color: Ink.line),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(children: rows),
    );
  }

  Widget _timelineRow(String time, String label, bool first) {
    return IntrinsicHeight(
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          SizedBox(
            width: 60,
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: 10),
              child: Text(
                time,
                style: const TextStyle(color: Ink.ink, fontSize: 15, fontWeight: FontWeight.w600),
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 12),
            child: Column(
              children: [
                Container(
                  width: 9,
                  height: 9,
                  decoration: BoxDecoration(
                    shape: BoxShape.circle,
                    color: first ? Ink.accent : Colors.white,
                    border: Border.all(color: first ? Ink.accent : Ink.muted, width: 2),
                  ),
                ),
                Expanded(
                  child: Container(width: 2, color: Ink.line),
                ),
              ],
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: 8),
              child: Text(
                label,
                style: const TextStyle(color: Ink.ink, fontSize: 15),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _confirmButton(TripDetail trip) {
    return FilledButton(
      onPressed: _confirmed || _busy ? null : () => _confirm(trip),
      child: Text(_confirmed ? 'Επιβεβαιώθηκε' : 'Έγινε η διαδρομή'),
    );
  }

  /// The people the caller may still rate, after the trip.
  Widget _ratingCard(TripDetail trip, List<Participant> toRate) {
    return Container(
      margin: const EdgeInsets.only(top: 20),
      padding: const EdgeInsets.fromLTRB(16, 14, 16, 14),
      decoration: BoxDecoration(
        border: Border.all(color: Ink.line),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Align(
            alignment: Alignment.centerLeft,
            child: Text(
              'Βαθμολόγησε',
              style: TextStyle(color: Ink.ink, fontSize: 18, fontWeight: FontWeight.w700),
            ),
          ),
          const SizedBox(height: 10),
          for (final person in toRate) _ratingRow(trip, person),
        ],
      ),
    );
  }

  Widget _ratingRow(TripDetail trip, Participant person) {
    if (person.rated) {
      return Padding(
        padding: const EdgeInsets.only(bottom: 10),
        child: Row(
          children: [
            Expanded(
              child: Text(
                person.name,
                style: const TextStyle(color: Ink.ink, fontSize: 15, fontWeight: FontWeight.w600),
              ),
            ),
            const Text('Βαθμολογήθηκε', style: TextStyle(color: Ink.muted, fontSize: 13)),
          ],
        ),
      );
    }
    final chosen = _stars[person.id] ?? 0;
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            person.name,
            style: const TextStyle(color: Ink.ink, fontSize: 15, fontWeight: FontWeight.w600),
          ),
          Row(
            children: [
              for (var n = 1; n <= 5; n++)
                IconButton(
                  key: Key('star-${person.id}-$n'),
                  tooltip: n == 1 ? '1 αστέρι' : '$n αστέρια',
                  icon: Icon(n <= chosen ? Icons.star : Icons.star_border),
                  color: n <= chosen ? Ink.accent : Ink.muted,
                  onPressed: _ratingBusy ? null : () => setState(() => _stars[person.id] = n),
                ),
            ],
          ),
          FilledButton(
            onPressed: chosen == 0 || _ratingBusy ? null : () => _rate(trip, person.id),
            child: const Text('Στείλε βαθμολογία'),
          ),
        ],
      ),
    );
  }

  /// The rating card, when there is somebody to rate.
  List<Widget> _ratingSection(TripDetail trip) {
    final people = trip.role == TripRole.passenger ? [trip.driver] : trip.passengers;
    final toRate = [for (final p in people) if (p.canRate || p.rated) p];
    if (toRate.isEmpty) return const [];
    return [_ratingCard(trip, toRate)];
  }

  // ------------------------------------------------------------------- driver

  List<Widget> _driverBody(TripDetail trip) {
    final rows = <Widget>[
      const Text(
        'Οι επιβάτες σου',
        style: TextStyle(color: Ink.ink, fontSize: 22, fontWeight: FontWeight.w700),
      ),
      const SizedBox(height: 12),
    ];
    for (final passenger in trip.passengers) {
      TripStop? stop;
      for (final s in trip.stops) {
        if (s.kind == 'pickup' && s.passengerId == passenger.id) {
          stop = s;
          break;
        }
      }
      rows.add(
        Padding(
          padding: const EdgeInsets.only(bottom: 14),
          child: InkWell(
            key: Key('person-${passenger.id}'),
            onTap: () => _openPerson(passenger),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: Text(
                    passenger.name,
                    style: const TextStyle(color: Ink.ink, fontSize: 16, fontWeight: FontWeight.w600),
                  ),
                ),
                Column(
                  crossAxisAlignment: CrossAxisAlignment.end,
                  children: [
                    if (stop?.label != null)
                      Text(stop!.label!, style: const TextStyle(color: Ink.muted, fontSize: 14)),
                    if (stop?.at != null)
                      Text(athensHm(stop!.at!), style: const TextStyle(color: Ink.muted, fontSize: 13)),
                  ],
                ),
              ],
            ),
          ),
        ),
      );
    }
    if (trip.passengers.isEmpty) {
      rows.add(const Text('Κανείς ακόμα', style: TextStyle(color: Ink.muted)));
      rows.add(const SizedBox(height: 8));
    }

    rows.add(const SizedBox(height: 8));
    if (_cancelled(trip)) {
      // Called off: nothing left to start, end, call off or confirm.
    } else if (trip.driveSetAt == null) {
      rows.add(
        FilledButton(
          onPressed: _busy ? null : () => _startDrive(trip),
          child: const Text('Φεύγω τώρα'),
        ),
      );
      rows.add(const SizedBox(height: 10));
      if (_before(trip)) {
        rows.add(
          OutlinedButton(
            style: OutlinedButton.styleFrom(foregroundColor: Ink.danger),
            onPressed: _busy ? null : () => _cancelTrip(trip),
            child: const Text('Ακύρωση διαδρομής'),
          ),
        );
        rows.add(const SizedBox(height: 10));
      }
    } else if (trip.driveEndedAt == null) {
      rows.add(
        OutlinedButton(
          onPressed: _busy ? null : () => _endDrive(trip),
          child: const Text('Τέλος διαδρομής'),
        ),
      );
      rows.add(const SizedBox(height: 10));
    }
    if (!_cancelled(trip)) rows.add(_confirmButton(trip));
    rows.addAll(_ratingSection(trip));
    rows.add(const SizedBox(height: 20));
    rows.add(
      TripChat(
        key: ValueKey(trip.id),
        api: widget.api,
        tripId: trip.id,
        myId: trip.myId,
        poll: widget.chatPoll,
      ),
    );
    return rows;
  }
}

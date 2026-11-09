import 'dart:async';

import 'package:flutter/material.dart';

import '../core/athens_time.dart';
import '../data/api.dart';
import '../data/models.dart';
import '../platform/drive_tracker.dart';
import 'app_scope.dart';
import 'messages.dart';
import 'trip_screen.dart';

/// The receipt for a seat somebody asked for, kept alive while drivers are
/// asked one at a time.
class RequestScreen extends StatefulWidget {
  const RequestScreen({super.key, required this.api, required this.requestId});

  final Api api;
  final String requestId;

  @override
  State<RequestScreen> createState() => _RequestScreenState();
}

class _RequestScreenState extends State<RequestScreen> {
  Timer? _poll;

  OpenRequest? _request;
  TripSummary? _matched;

  bool _loaded = false;
  bool _failed = false;
  String? _outcome;

  @override
  void initState() {
    super.initState();
    _refresh();
    // Every fifteen seconds: the count of drivers asked climbs while somebody
    // sits here, and a driver accepting is what closes the request.
    _poll = Timer.periodic(const Duration(seconds: 15), (_) => _refresh());
  }

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  Future<void> _refresh() async {
    try {
      final requests = await widget.api.myRequests();
      if (!mounted) return;
      OpenRequest? mine;
      for (final request in requests) {
        if (request.id == widget.requestId) mine = request;
      }
      if (mine != null) {
        setState(() {
          _request = mine;
          _matched = null;
          _loaded = true;
          _failed = false;
        });
        return;
      }
      // It left the open list: a driver took it, or it was cancelled or
      // expired. Only the first of those has somewhere to go.
      final trips = await widget.api.upcomingTrips();
      if (!mounted) return;
      TripSummary? match;
      final known = _request;
      if (known != null) {
        for (final trip in trips) {
          if (trip.originLabel == known.originLabel &&
              trip.destinationLabel == known.destinationLabel) {
            match = trip;
            break;
          }
        }
      }
      setState(() {
        _request = null;
        _matched = match;
        _loaded = true;
        _failed = false;
      });
    } catch (_) {
      if (!mounted) return;
      // A failed poll keeps whatever the screen already shows. Only a first
      // load that fails replaces the request with news that it could not be
      // read.
      setState(() {
        if (!_loaded) _failed = true;
      });
    }
  }

  Future<void> _cancel() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Ακύρωση αιτήματος'),
        content: const Text('Σταματάμε να ψάχνουμε οδηγό για αυτή τη διαδρομή.'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('Κράτα το'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('Ακύρωση'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    try {
      await widget.api.cancelRequest(widget.requestId);
      if (!mounted) return;
      Navigator.of(context).pop();
    } catch (error) {
      if (!mounted) return;
      // A driver accepting mid-press is ordinary rather than rare, and then the
      // seat is held in their name: the button must not offer to cancel again.
      setState(() => _outcome = failureText(error));
      _refresh();
    }
  }

  Future<void> _openTrip(String id) async {
    final scope = AppScope.maybeOf(context);
    if (scope != null) {
      scope.openRoute('/trip/$id');
      return;
    }
    await Navigator.of(context).push(MaterialPageRoute<void>(
      builder: (_) => TripScreen(
        api: widget.api,
        tripId: id,
        onStartDrive: (trip) => DriveTracker.start(widget.api, trip.id),
      ),
    ));
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final request = _request;
    return Scaffold(
      appBar: AppBar(title: const Text('Το αίτημά σου')),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              if (!_loaded && !_failed)
                const Padding(
                  padding: EdgeInsets.all(24),
                  child: Center(child: CircularProgressIndicator()),
                )
              else if (_failed && request == null)
                Text('Δεν φόρτωσε το αίτημά σου.', style: TextStyle(color: theme.colorScheme.error))
              else if (request != null) ...[
                Text(
                  '${request.originLabel} → ${request.destinationLabel}',
                  style: theme.textTheme.titleMedium,
                ),
                const SizedBox(height: 6),
                Text(
                  '${athensDay(request.windowStart)} · '
                  '${athensHm(request.windowStart)}–${athensHm(request.windowEnd)}'
                  ' · ${request.seats} ${request.seats == 1 ? "θέση" : "θέσεις"}',
                  style: theme.textTheme.bodyMedium,
                ),
                const SizedBox(height: 20),
                Row(
                  children: [
                    Container(
                      width: 10,
                      height: 10,
                      decoration: BoxDecoration(
                        color: theme.colorScheme.primary,
                        shape: BoxShape.circle,
                      ),
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        request.askingNow
                            ? 'Ρωτάμε οδηγό… (${request.driversAsked} ρωτήθηκαν)'
                            : 'Περιμένουμε οδηγό στον δρόμο σου',
                        style: theme.textTheme.titleSmall,
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 12),
                Text(
                  'Μπορείς να κλείσεις την εφαρμογή. Θα σου έρθει ειδοποίηση μόλις '
                  'κάποιος οδηγός δεχτεί.',
                  style: theme.textTheme.bodySmall,
                ),
              ] else ...[
                Text(
                  _matched != null
                      ? 'Ένας οδηγός ανέλαβε τη διαδρομή σου.'
                      : 'Αυτό το αίτημα δεν είναι πια ανοιχτό.',
                  style: theme.textTheme.titleMedium,
                ),
                if (_matched != null) ...[
                  const SizedBox(height: 8),
                  Text(
                    '${_matched!.originLabel} → ${_matched!.destinationLabel} · '
                    '${athensHm(_matched!.departEarliest)}',
                    style: theme.textTheme.bodyMedium,
                  ),
                ],
              ],
              if (_outcome != null) ...[
                const SizedBox(height: 12),
                Text(_outcome!, style: TextStyle(color: theme.colorScheme.error)),
              ],
              const SizedBox(height: 24),
              if (request != null)
                OutlinedButton(
                  onPressed: _cancel,
                  child: const Text('Ακύρωση αιτήματος'),
                ),
              if (_matched != null) ...[
                const SizedBox(height: 12),
                FilledButton(
                  onPressed: () => _openTrip(_matched!.id),
                  child: const Text('Άνοιξε τη διαδρομή'),
                ),
              ],
              const SizedBox(height: 12),
              OutlinedButton(
                onPressed: () => Navigator.of(context).pop(),
                child: const Text('Πίσω στην αρχική'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

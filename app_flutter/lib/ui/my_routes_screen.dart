import 'package:flutter/material.dart' hide Ink;

import '../core/format.dart';
import '../data/api.dart';
import '../data/models.dart';
import 'messages.dart';
import 'theme.dart';

/// The weekday names the list uses, ISO 1 = Monday.
const List<String> _dayNames = ['Δευ', 'Τρί', 'Τετ', 'Πέμ', 'Παρ', 'Σάβ', 'Κυρ'];

/// The routes the caller published, each with the nudge switch and the way to
/// retire it.
class MyRoutesScreen extends StatefulWidget {
  const MyRoutesScreen({super.key, required this.api});

  final Api api;

  @override
  State<MyRoutesScreen> createState() => _MyRoutesScreenState();
}

class _MyRoutesScreenState extends State<MyRoutesScreen> {
  List<MyRoute> _routes = const [];
  bool _loading = true;
  String? _error;

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
      final routes = await widget.api.myRoutes();
      if (!mounted) return;
      setState(() {
        _routes = routes;
        _loading = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _error = failureText(error);
        _loading = false;
      });
    }
  }

  Future<void> _setNudge(MyRoute route, bool on) async {
    final index = _routes.indexWhere((r) => r.id == route.id);
    if (index < 0) return;
    final before = route.nudgeWhenLeaving;
    setState(() {
      _routes = [
        for (final r in _routes)
          r.id == route.id ? r.withNudge(on) : r,
      ];
    });
    try {
      await widget.api.setNudge(route.id, on);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _routes = [
          for (final r in _routes)
            r.id == route.id ? r.withNudge(before) : r,
        ];
      });
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(failureText(error))));
    }
  }

  Future<void> _retire(MyRoute route) async {
    final kept = route.keptTrips;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Απόσυρση διαδρομής;'),
        content: Text(
          kept > 0
              ? 'Η διαδρομή δεν θα προσφέρεται πια και τα άδεια δρομολόγιά της '
                  'ακυρώνονται. Μένουν $kept με επιβάτες: ακύρωσέ τα από το '
                  'καθένα αν χρειάζεται, για να ειδοποιηθούν.'
              : 'Η διαδρομή δεν θα προσφέρεται πια και τα προγραμματισμένα '
                  'δρομολόγιά της ακυρώνονται.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('Άκυρο'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('Απόσυρση'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    try {
      await widget.api.retireRoute(route.id);
      if (!mounted) return;
      await _load();
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(failureText(error))));
    }
  }

  String _when(MyRoute route) {
    final days = [
      for (final day in route.weekdays)
        if (day >= 1 && day <= 7) _dayNames[day - 1],
    ];
    if (days.isNotEmpty) return days.join(', ');
    final date = route.oneoffDate;
    if (date != null) return date;
    return '';
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: const Text('Οι διαδρομές μου')),
      body: SafeArea(
        child: _loading
            ? const Padding(
                padding: EdgeInsets.all(24),
                child: Text('Φόρτωση…'),
              )
            : _error != null
                ? Padding(
                    padding: const EdgeInsets.all(16),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(_error!, style: TextStyle(color: theme.colorScheme.error)),
                        const SizedBox(height: 12),
                        OutlinedButton(
                          onPressed: _load,
                          child: const Text('Δοκίμασε ξανά'),
                        ),
                      ],
                    ),
                  )
                : _routes.isEmpty
                    ? const Padding(
                        padding: EdgeInsets.all(24),
                        child: Text('Δεν έχεις δημοσιεύσει διαδρομές'),
                      )
                    : ListView(
                        padding: const EdgeInsets.all(16),
                        children: [
                          for (final route in _routes) _card(theme, route),
                        ],
                      ),
      ),
    );
  }

  Widget _card(ThemeData theme, MyRoute route) {
    final when = _when(route);
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      color: Colors.white,
      elevation: 0,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(16),
        side: const BorderSide(color: Ink.line),
      ),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '${route.departEarliest}–${route.departLatest}',
              style: theme.textTheme.titleMedium,
            ),
            const SizedBox(height: 4),
            Text('${route.originLabel} → ${route.destinationLabel}'),
            if (when.isNotEmpty) ...[
              const SizedBox(height: 4),
              Text(when, style: const TextStyle(color: Ink.muted)),
            ],
            const SizedBox(height: 4),
            Text(
              '${kmTextNumber(route.routeKm)} χλμ · ${route.seats} θέσεις',
              style: const TextStyle(color: Ink.muted),
            ),
            if (route.upcomingTrips > 0) ...[
              const SizedBox(height: 8),
              Text(
                route.upcomingTrips == 1
                    ? '${route.upcomingTrips} προγραμματισμένο δρομολόγιο'
                    : '${route.upcomingTrips} προγραμματισμένα δρομολόγια',
                style: const TextStyle(color: Ink.accent, fontWeight: FontWeight.w600),
              ),
            ],
            if (route.linkOpens > 0) ...[
              const SizedBox(height: 8),
              Text(
                route.linkOpens == 1
                    ? 'Ο σύνδεσμος άνοιξε ${route.linkOpens} φορά'
                    : 'Ο σύνδεσμος άνοιξε ${route.linkOpens} φορές',
                style: const TextStyle(color: Ink.muted),
              ),
            ],
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('Πες μου όταν κάποιος στον δρόμο μου περιμένει'),
              subtitle: const Text('Λίγο πριν φύγεις, αν έχεις ελεύθερη θέση.'),
              value: route.nudgeWhenLeaving,
              onChanged: (on) => _setNudge(route, on),
            ),
            TextButton(
              onPressed: () => _retire(route),
              child: const Text('Απόσυρση'),
            ),
          ],
        ),
      ),
    );
  }
}

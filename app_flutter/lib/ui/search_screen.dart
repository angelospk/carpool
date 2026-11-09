import 'package:flutter/material.dart';
import 'package:timezone/timezone.dart' as tz;

import '../core/athens_time.dart';
import '../data/api.dart';
import '../data/models.dart';
import 'messages.dart';
import 'place_search.dart';
import 'request_screen.dart';

/// Asking for a seat. Two places and a time window are the whole question; the
/// server decides which drivers on that road are worth asking.
class SearchScreen extends StatefulWidget {
  const SearchScreen({super.key, required this.api, this.onRequested});

  final Api api;
  final void Function(String requestId)? onRequested;

  @override
  State<SearchScreen> createState() => _SearchScreenState();
}

class _SearchScreenState extends State<SearchScreen> {
  Place? _from;
  Place? _to;

  /// The day, at midnight Athens time. Today to begin with, read when the
  /// screen is built rather than when the app started.
  late DateTime _day;
  late TimeOfDay _start;
  late TimeOfDay _end;

  int _seats = 1;
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    // Round up to the next quarter hour: "now" is a request a driver can still
    // take. A quarter hour that crosses midnight also moves the day.
    final now = athensNow();
    final minutesToAdd = 15 - (now.minute % 15);
    final rounded = now.add(Duration(minutes: minutesToAdd == 15 ? 0 : minutesToAdd));
    _day = DateTime(rounded.year, rounded.month, rounded.day);
    _start = TimeOfDay(hour: rounded.hour, minute: rounded.minute);
    _end = TimeOfDay.fromDateTime(rounded.add(const Duration(minutes: 60)));
  }

  tz.TZDateTime _athensOn(DateTime day, TimeOfDay time) {
    return tz.TZDateTime(
      tz.getLocation('Europe/Athens'),
      day.year,
      day.month,
      day.day,
      time.hour,
      time.minute,
    );
  }

  bool get _ready => _from != null && _to != null && !_busy;

  String _hm(TimeOfDay time) =>
      '${time.hour.toString().padLeft(2, '0')}:${time.minute.toString().padLeft(2, '0')}';

  Future<void> _pickFrom() async {
    final place = await pickPlace(context, widget.api, title: 'Από');
    if (place == null || !mounted) return;
    setState(() => _from = place);
  }

  Future<void> _pickTo() async {
    final place = await pickPlace(context, widget.api, title: 'Προς');
    if (place == null || !mounted) return;
    setState(() => _to = place);
  }

  Future<void> _pickDay() async {
    final today = DateTime.now();
    final picked = await showDatePicker(
      context: context,
      initialDate: _day,
      firstDate: DateTime(today.year, today.month, today.day),
      lastDate: DateTime(today.year, today.month, today.day + 21),
      helpText: 'Πότε',
    );
    if (picked == null || !mounted) return;
    setState(() => _day = DateTime(picked.year, picked.month, picked.day));
  }

  Future<void> _pickTime({required bool start}) async {
    final picked = await showTimePicker(
      context: context,
      initialTime: start ? _start : _end,
      helpText: start ? 'Από' : 'Έως',
    );
    if (picked == null || !mounted) return;
    setState(() {
      if (start) {
        _start = picked;
      } else {
        _end = picked;
      }
    });
  }

  Future<void> _submit() async {
    final from = _from;
    final to = _to;
    if (from == null || to == null || _busy) return;

    final start = _athensOn(_day, _start);
    var end = _athensOn(_day, _end);
    // 23:30 to 00:30 is an hour that ends the next day, not a backwards one.
    if (end.isBefore(start)) end = _athensOn(_day.add(const Duration(days: 1)), _end);
    if (!end.isAfter(start)) {
      setState(() => _error = 'Η τελευταία ώρα πρέπει να είναι μετά την πρώτη.');
      return;
    }
    if (end.isBefore(athensNow())) {
      setState(() => _error = 'Αυτή η ώρα έχει ήδη περάσει. Διάλεξε μελλοντική ώρα.');
      return;
    }

    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final result = await widget.api.createRequest(
        origin: from,
        destination: to,
        windowStart: start,
        windowEnd: end,
        seats: _seats,
      );
      if (!mounted) return;
      final onRequested = widget.onRequested;
      if (onRequested != null) {
        onRequested(result.requestId);
      } else {
        Navigator.of(context).push(MaterialPageRoute<void>(
          builder: (_) => RequestScreen(api: widget.api, requestId: result.requestId),
        ));
      }
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = failureText(error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final dayText = '${_day.day}/${_day.month}/${_day.year}';
    return Scaffold(
      appBar: AppBar(title: const Text('Πού πας;')),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              _PlaceField(
                label: 'Από',
                place: _from,
                onPick: _busy ? null : _pickFrom,
              ),
              const SizedBox(height: 12),
              _PlaceField(
                label: 'Προς',
                place: _to,
                onPick: _busy ? null : _pickTo,
              ),
              const SizedBox(height: 20),
              ListTile(
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.event),
                title: const Text('Μέρα'),
                subtitle: Text(dayText),
                trailing: TextButton(
                  onPressed: _busy ? null : _pickDay,
                  child: const Text('Αλλαγή'),
                ),
              ),
              Row(
                children: [
                  Expanded(
                    child: ListTile(
                      contentPadding: EdgeInsets.zero,
                      title: const Text('Από ώρα'),
                      subtitle: Text(_hm(_start)),
                      onTap: _busy ? null : () => _pickTime(start: true),
                    ),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: ListTile(
                      contentPadding: EdgeInsets.zero,
                      title: const Text('Έως ώρα'),
                      subtitle: Text(_hm(_end)),
                      onTap: _busy ? null : () => _pickTime(start: false),
                    ),
                  ),
                ],
              ),
              const Divider(height: 32),
              Row(
                children: [
                  const Expanded(child: Text('Θέσεις')),
                  IconButton(
                    onPressed: _seats > 1 && !_busy ? () => setState(() => _seats--) : null,
                    icon: const Icon(Icons.remove_circle_outline),
                    tooltip: 'Μία λιγότερη',
                  ),
                  Text('$_seats', style: theme.textTheme.titleMedium),
                  IconButton(
                    onPressed: _seats < 4 && !_busy ? () => setState(() => _seats++) : null,
                    icon: const Icon(Icons.add_circle_outline),
                    tooltip: 'Μία περισσότερη',
                  ),
                ],
              ),
              const SizedBox(height: 12),
              Text(
                'Θα ρωτήσουμε έναν έναν τους οδηγούς που πάνε προς τα εκεί και θα σε '
                'ειδοποιήσουμε μόλις κάποιος δεχτεί. Δεν χρεώνεσαι τίποτα.',
                style: theme.textTheme.bodySmall,
              ),
              if (_error != null) ...[
                const SizedBox(height: 12),
                Text(_error!, style: TextStyle(color: theme.colorScheme.error)),
              ],
              const SizedBox(height: 24),
              FilledButton(
                onPressed: _ready ? _submit : null,
                child: _busy
                    ? const SizedBox(
                        height: 20,
                        width: 20,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Text('Ζήτα θέση'),
              ),
              const SizedBox(height: 32),
            ],
          ),
        ),
      ),
    );
  }
}

/// One end of the journey: the place picked, or an invitation to pick one.
class _PlaceField extends StatelessWidget {
  const _PlaceField({required this.label, required this.place, required this.onPick});

  final String label;
  final Place? place;
  final VoidCallback? onPick;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return InkWell(
      onTap: onPick,
      borderRadius: BorderRadius.circular(8),
      child: InputDecorator(
        decoration: InputDecoration(
          labelText: label,
          border: const OutlineInputBorder(),
          suffixIcon: const Icon(Icons.search),
        ),
        child: Text(
          place == null
              ? 'Γράψε μια πόλη ή χωριό'
              : place!.area.isEmpty
                  ? place!.name
                  : '${place!.name} · ${place!.area}',
          style: place == null ? theme.textTheme.bodyMedium?.copyWith(
            color: theme.textTheme.bodyMedium?.color?.withValues(alpha: 0.6),
          ) : theme.textTheme.bodyMedium,
        ),
      ),
    );
  }
}

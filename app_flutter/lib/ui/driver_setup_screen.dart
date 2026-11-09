import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../data/api.dart';
import 'messages.dart';

/// The car a driver describes, so that the server can price what it drinks.
///
/// Nothing here is a judgement the app makes: make, model, fuel and the
/// displacement are read off the άδεια κυκλοφορίας, and the consumption is
/// worked out from them. LPG is not offered because the server does not price
/// it: the dataset has no LPG figures whose unit is certain.
class DriverSetupScreen extends StatefulWidget {
  const DriverSetupScreen({super.key, required this.api});

  final Api api;

  @override
  State<DriverSetupScreen> createState() => _DriverSetupScreenState();
}

class _DriverSetupScreenState extends State<DriverSetupScreen> {
  final TextEditingController _make = TextEditingController();
  final TextEditingController _model = TextEditingController();
  final TextEditingController _engineCc = TextEditingController();
  final TextEditingController _colour = TextEditingController();
  final TextEditingController _plate = TextEditingController();

  String _fuel = 'petrol';
  bool _busy = false;
  String? _error;

  static const Map<String, String> _fuels = {
    'petrol': 'Βενζίνη',
    'diesel': 'Ντίζελ',
    'petrol_hybrid': 'Υβριδικό βενζίνης',
    'diesel_hybrid': 'Υβριδικό ντίζελ',
  };

  @override
  void dispose() {
    _make.dispose();
    _model.dispose();
    _engineCc.dispose();
    _colour.dispose();
    _plate.dispose();
    super.dispose();
  }

  int get _cc => int.tryParse(_engineCc.text) ?? 0;

  bool get _ready =>
      _make.text.trim().isNotEmpty &&
      _model.text.trim().isNotEmpty &&
      _plate.text.trim().isNotEmpty &&
      _cc > 0 &&
      _cc < 10000 &&
      !_busy;

  /// Saves the car. The displacement goes with it: the server prices a car
  /// from make, model, fuel and cm³ and refuses one it cannot place.
  Future<void> _save() => widget.api.putDriver(
        make: _make.text.trim(),
        model: _model.text.trim(),
        fuel: _fuel,
        engineCc: _cc,
        colour: _colour.text.trim(),
        plate: _plate.text.trim(),
      );

  Future<void> _submit() async {
    if (!_ready) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await _save();
      if (!mounted) return;
      Navigator.of(context).pop(true);
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = _refusal(error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// A refused car is usually one the server cannot place: an unknown make, a
  /// model it has never seen and no displacement that is filled in.
  String _refusal(Object error) {
    if (error is ApiException && error.code == 'invalid_request') {
      final message = error.message;
      if (message.contains('make and a model')) return 'Γράψε μάρκα και μοντέλο.';
      if (message.contains('plate')) return 'Χρειάζεται πινακίδα, για να βρει το αυτοκίνητο ο επιβάτης.';
      if (message.contains('fuel')) return 'Διάλεξε καύσιμο από τη λίστα.';
      return 'Δεν μπορώ να υπολογίσω την κατανάλωση αυτού του αυτοκινήτου. '
          'Έλεγξε μάρκα, μοντέλο, καύσιμο και κυβικά.';
    }
    return failureText(error);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: const Text('Το αυτοκίνητό μου')),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(
                'Γράψε ό,τι λέει η άδεια κυκλοφορίας. Δεν ρωτάμε πόσο πίνει: το '
                'βρίσκουμε εμείς, γι\' αυτό η συνεισφορά μένει κάτω από το κόστος.',
                style: theme.textTheme.bodySmall,
              ),
              const SizedBox(height: 16),
              TextField(
                controller: _make,
                textCapitalization: TextCapitalization.words,
                onChanged: (_) => setState(() {}),
                decoration: const InputDecoration(
                  labelText: 'Μάρκα',
                  hintText: 'Toyota',
                  border: OutlineInputBorder(),
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _model,
                textCapitalization: TextCapitalization.words,
                onChanged: (_) => setState(() {}),
                decoration: const InputDecoration(
                  labelText: 'Μοντέλο',
                  hintText: 'Yaris',
                  border: OutlineInputBorder(),
                ),
              ),
              const SizedBox(height: 16),
              Text('Καύσιμο', style: theme.textTheme.labelLarge),
              const SizedBox(height: 8),
              SegmentedButton<String>(
                segments: [
                  for (final entry in _fuels.entries)
                    ButtonSegment<String>(value: entry.key, label: Text(entry.value)),
                ],
                selected: {_fuel},
                onSelectionChanged: _busy
                    ? null
                    : (selection) => setState(() => _fuel = selection.first),
                showSelectedIcon: false,
              ),
              const SizedBox(height: 16),
              TextField(
                controller: _engineCc,
                keyboardType: TextInputType.number,
                inputFormatters: [FilteringTextInputFormatter.digitsOnly],
                onChanged: (_) => setState(() {}),
                decoration: const InputDecoration(
                  labelText: 'Κυβικά',
                  hintText: '1329',
                  border: OutlineInputBorder(),
                  helperText: 'Από την άδεια κυκλοφορίας, όχι στρογγυλεμένα.',
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _colour,
                textCapitalization: TextCapitalization.words,
                decoration: const InputDecoration(
                  labelText: 'Χρώμα',
                  hintText: 'λευκό',
                  border: OutlineInputBorder(),
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _plate,
                textCapitalization: TextCapitalization.characters,
                autocorrect: false,
                onChanged: (_) => setState(() {}),
                decoration: const InputDecoration(
                  labelText: 'Πινακίδα',
                  hintText: 'ΑΒΓ-1234',
                  border: OutlineInputBorder(),
                ),
              ),
              if (_error != null) ...[
                const SizedBox(height: 16),
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
                    : const Text('Αποθήκευση'),
              ),
              const SizedBox(height: 24),
            ],
          ),
        ),
      ),
    );
  }
}

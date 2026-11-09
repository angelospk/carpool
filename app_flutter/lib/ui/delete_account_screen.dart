import 'package:flutter/material.dart' hide Ink;
import 'package:flutter/services.dart';

import '../data/api.dart';
import 'messages.dart';

/// The last step of deletion: the SMS code, and the button that ends it.
class DeleteAccountScreen extends StatefulWidget {
  const DeleteAccountScreen({super.key, required this.api, required this.onDeleted});

  final Api api;

  /// Where a deleted account lands: the app's signed-out screen.
  final Future<void> Function() onDeleted;

  @override
  State<DeleteAccountScreen> createState() => _DeleteAccountScreenState();
}

class _DeleteAccountScreenState extends State<DeleteAccountScreen> {
  final TextEditingController _code = TextEditingController();

  bool _loading = true;
  bool _busy = false;
  String? _error;

  /// The server did not send a code, so the screen must not say it did.
  bool _sendFailed = false;

  @override
  void initState() {
    super.initState();
    _requestCode();
  }

  @override
  void dispose() {
    _code.dispose();
    super.dispose();
  }

  /// Asks for the code once, and fills it in while the server is in dev mode.
  Future<void> _requestCode() async {
    try {
      final devCode = await widget.api.requestDeleteCode();
      if (!mounted) return;
      setState(() {
        if (devCode != null) _code.text = devCode;
        _loading = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _sendFailed = true;
        _error = failureText(error);
      });
    }
  }

  bool get _ready => _code.text.length == 6 && !_busy;

  Future<void> _delete() async {
    if (!_ready) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.api.deleteAccount(_code.text);
      if (!mounted) return;
      await widget.onDeleted();
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() => _error = failureText(e));
    } catch (_) {
      if (!mounted) return;
      setState(() => _error = 'Κάτι πήγε στραβά');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: const Text('Διαγραφή λογαριασμού')),
      body: SafeArea(
        child: _loading
            ? const Padding(
                padding: EdgeInsets.all(24),
                child: Text('Φόρτωση…'),
              )
            : SingleChildScrollView(
                padding: const EdgeInsets.all(16),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    if (!_sendFailed) const Text('Σου στείλαμε κωδικό με SMS'),
                    const SizedBox(height: 12),
                    TextField(
                      controller: _code,
                      keyboardType: TextInputType.number,
                      inputFormatters: [
                        FilteringTextInputFormatter.digitsOnly,
                        LengthLimitingTextInputFormatter(6),
                      ],
                      decoration: const InputDecoration(labelText: 'Κωδικός'),
                      enabled: !_busy,
                      onChanged: (_) => setState(() {}),
                    ),
                    if (_error != null) ...[
                      const SizedBox(height: 12),
                      Text(_error!, style: TextStyle(color: theme.colorScheme.error)),
                    ],
                    const SizedBox(height: 20),
                    FilledButton(
                      onPressed: _ready ? _delete : null,
                      child: const Text('Διαγραφή λογαριασμού'),
                    ),
                  ],
                ),
              ),
      ),
    );
  }
}

import 'package:flutter/material.dart' hide Ink;

import '../data/api.dart';
import '../data/models.dart';
import 'messages.dart';

/// Everybody the caller has blocked, and the way back.
class BlockedScreen extends StatefulWidget {
  const BlockedScreen({super.key, required this.api});

  final Api api;

  @override
  State<BlockedScreen> createState() => _BlockedScreenState();
}

class _BlockedScreenState extends State<BlockedScreen> {
  List<BlockedUser> _blocked = const [];
  bool _loading = true;
  bool _busy = false;
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
      final blocked = await widget.api.blocks();
      if (!mounted) return;
      setState(() {
        _blocked = blocked;
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

  Future<void> _unblock(BlockedUser user) async {
    if (_busy) return;
    setState(() => _busy = true);
    try {
      await widget.api.unblock(user.id);
      if (!mounted) return;
      await _load();
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(failureText(error))));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: const Text('Αποκλεισμένοι')),
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
                : _blocked.isEmpty
                    ? const Padding(
                        padding: EdgeInsets.all(24),
                        child: Text('Δεν έχεις αποκλείσει κανέναν.'),
                      )
                    : ListView(
                        padding: const EdgeInsets.all(8),
                        children: [
                          for (final user in _blocked)
                            ListTile(
                              title: Text(user.name.isEmpty ? 'Χρήστης' : user.name),
                              trailing: TextButton(
                                onPressed: _busy ? null : () => _unblock(user),
                                child: const Text('Άρση αποκλεισμού'),
                              ),
                            ),
                        ],
                      ),
      ),
    );
  }
}

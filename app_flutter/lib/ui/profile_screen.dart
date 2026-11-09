import 'package:flutter/material.dart';

import '../data/api.dart';
import '../data/models.dart';
import 'app_scope.dart';
import 'driver_setup_screen.dart';

/// Somebody signed in: who they are, and the two things they can do from here.
class ProfileScreen extends StatefulWidget {
  const ProfileScreen({super.key, required this.api, this.onSignOut});

  final Api api;

  /// Where signing out lands. The app's own root screen when it is wired to
  /// one; a popped screen when it is not.
  final Future<void> Function()? onSignOut;

  @override
  State<ProfileScreen> createState() => _ProfileScreenState();
}

class _ProfileScreenState extends State<ProfileScreen> {
  Me? _me;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final me = await widget.api.me();
      if (!mounted) return;
      setState(() => _me = me);
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = 'Δεν φόρτωσε το προφίλ σου.');
    }
  }

  Future<void> _openDriverSetup() async {
    final saved = await Navigator.of(context).push<bool>(MaterialPageRoute<bool>(
      builder: (_) => DriverSetupScreen(api: widget.api),
    ));
    if (saved == true && mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Το αυτοκίνητο αποθηκεύτηκε.')),
      );
    }
  }

  Future<void> _signOut() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Αποσύνδεση'),
        content: const Text('Θα χρειαστείς νέο κωδικό SMS για να ξαναμπείς.'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('Κράτα με'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('Αποσύνδεση'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    final onSignOut = widget.onSignOut;
    if (onSignOut != null) {
      await onSignOut();
      return;
    }
    final scope = AppScope.maybeOf(context);
    await widget.api.signOut();
    if (!mounted) return;
    if (scope != null) {
      scope.onSignedOut();
    } else {
      Navigator.of(context).pop();
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final me = _me;
    return Scaffold(
      appBar: AppBar(title: const Text('Το προφίλ σου')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            if (_error != null)
              Text(_error!, style: TextStyle(color: theme.colorScheme.error))
            else if (me == null)
              const Padding(
                padding: EdgeInsets.all(24),
                child: Center(child: CircularProgressIndicator()),
              )
            else ...[
              ListTile(
                contentPadding: EdgeInsets.zero,
                title: Text(me.name.isEmpty ? 'Χωρίς όνομα' : me.name,
                    style: theme.textTheme.titleLarge),
                subtitle: Text(me.phone),
              ),
              const Divider(height: 32),
              ListTile(
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.directions_car),
                title: const Text('Το αυτοκίνητό μου'),
                subtitle: const Text('Μάρκα, μοντέλο, καύσιμο, πινακίδα'),
                trailing: const Icon(Icons.chevron_right),
                onTap: _openDriverSetup,
              ),
              const Divider(height: 32),
              OutlinedButton(
                onPressed: _signOut,
                child: const Text('Αποσύνδεση'),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

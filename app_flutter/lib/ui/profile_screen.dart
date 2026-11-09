import 'package:flutter/material.dart' hide Ink;

import '../data/api.dart';
import '../data/models.dart';
import 'app_scope.dart';
import 'blocked_screen.dart';
import 'delete_account_screen.dart';
import 'driver_setup_screen.dart';
import 'my_routes_screen.dart';
import 'theme.dart';

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

  Future<void> _openMyRoutes() async {
    await Navigator.of(context).push<void>(MaterialPageRoute<void>(
      builder: (_) => MyRoutesScreen(api: widget.api),
    ));
  }

  Future<void> _openBlocked() async {
    await Navigator.of(context).push<void>(MaterialPageRoute<void>(
      builder: (_) => BlockedScreen(api: widget.api),
    ));
  }

  /// What signing out does, once somebody has said yes or deleted the account.
  Future<void> _leave() async {
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
    await _leave();
  }

  Future<void> _deleteAccount() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Διαγραφή λογαριασμού;'),
        content: const Text(
          'Ο λογαριασμός σου θα διαγραφεί για πάντα και δεν γίνεται επαναφορά. '
          'Θα σου στείλουμε κωδικό με SMS για να το επιβεβαιώσεις.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('Άκυρο'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('Διαγραφή λογαριασμού'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    await Navigator.of(context).push<void>(MaterialPageRoute<void>(
      builder: (_) => DeleteAccountScreen(api: widget.api, onDeleted: _leave),
    ));
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
                child: Center(child: Text('Φόρτωση…')),
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
              ListTile(
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.route),
                title: const Text('Οι διαδρομές μου'),
                trailing: const Icon(Icons.chevron_right),
                onTap: _openMyRoutes,
              ),
              ListTile(
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.block),
                title: const Text('Αποκλεισμένοι'),
                trailing: const Icon(Icons.chevron_right),
                onTap: _openBlocked,
              ),
              const Divider(height: 32),
              OutlinedButton(
                onPressed: _signOut,
                child: const Text('Αποσύνδεση'),
              ),
              const SizedBox(height: 12),
              TextButton(
                onPressed: _deleteAccount,
                style: TextButton.styleFrom(foregroundColor: Ink.danger),
                child: const Text('Διαγραφή λογαριασμού'),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

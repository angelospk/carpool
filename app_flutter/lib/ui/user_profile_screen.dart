import 'package:flutter/material.dart' hide Ink;

import '../data/api.dart';
import 'messages.dart';
import 'theme.dart';

/// The reasons a report offers, as the server calls them.
const List<({String id, String label})> reportReasons = [
  (id: 'harassment', label: 'Παρενόχληση ή προσβλητική συμπεριφορά'),
  (id: 'unsafe_driving', label: 'Επικίνδυνη οδήγηση'),
  (id: 'no_show', label: 'Δεν εμφανίστηκε'),
  (id: 'money', label: 'Ζήτησε παραπάνω χρήματα'),
  (id: 'fake_profile', label: 'Ψεύτικο προφίλ'),
  (id: 'other', label: 'Κάτι άλλο'),
];

/// Somebody else on a trip: who they are, whether they are blocked, and the
/// two things the caller may do about them.
class UserProfileScreen extends StatefulWidget {
  const UserProfileScreen({
    super.key,
    required this.api,
    required this.userId,
    required this.name,
  });

  final Api api;
  final String userId;
  final String name;

  @override
  State<UserProfileScreen> createState() => _UserProfileScreenState();
}

class _UserProfileScreenState extends State<UserProfileScreen> {
  bool _loading = true;
  bool _blocked = false;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  /// Reads the blocked list once, to know which of the two buttons to show.
  Future<void> _load() async {
    try {
      final blocked = await widget.api.blocks();
      if (!mounted) return;
      setState(() {
        _blocked = blocked.any((user) => user.id == widget.userId);
        _loading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _loading = false);
    }
  }

  void _say(String text) {
    final messenger = ScaffoldMessenger.of(context);
    messenger.hideCurrentSnackBar();
    messenger.showSnackBar(SnackBar(content: Text(text)));
  }

  Future<void> _toggleBlock() async {
    if (_busy) return;
    setState(() => _busy = true);
    final blocking = !_blocked;
    try {
      if (blocking) {
        await widget.api.block(widget.userId);
      } else {
        await widget.api.unblock(widget.userId);
      }
      if (!mounted) return;
      setState(() => _blocked = blocking);
      _say(blocking ? 'Ο χρήστης αποκλείστηκε' : 'Ο αποκλεισμός αναιρέθηκε');
    } catch (error) {
      if (!mounted) return;
      _say(failureText(error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _report() async {
    final reason = await showDialog<({String id, String detail})>(
      context: context,
      builder: (_) => const _ReportDialog(),
    );
    if (reason == null || !mounted) return;
    try {
      final body = reason.detail.isEmpty ? reason.id : '${reason.id}: ${reason.detail}';
      await widget.api.report(widget.userId, body);
      if (!mounted) return;
      _say('Η αναφορά στάλθηκε');
    } catch (error) {
      if (!mounted) return;
      _say(failureText(error));
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final name = widget.name.isEmpty ? 'Χρήστης' : widget.name;
    return Scaffold(
      appBar: AppBar(title: const Text('Προφίλ')),
      body: SafeArea(
        child: _loading
            ? const Padding(
                padding: EdgeInsets.all(24),
                child: Text('Φόρτωση…'),
              )
            : ListView(
                padding: const EdgeInsets.all(16),
                children: [
                  Text(name, style: theme.textTheme.titleLarge),
                  const Divider(height: 32),
                  FilledButton(
                    onPressed: _busy ? null : _toggleBlock,
                    style: FilledButton.styleFrom(
                      backgroundColor: _blocked ? Ink.muted : Ink.danger,
                    ),
                    child: Text(_blocked ? 'Άρση αποκλεισμού' : 'Αποκλεισμός'),
                  ),
                  const SizedBox(height: 12),
                  const Text(
                    'Δεν θα σας φέρουμε ξανά σε ίδιο ταξίδι. Ταξίδι που έχετε ήδη '
                    'κλείσει δεν ακυρώνεται: αυτό γίνεται από το ίδιο το ταξίδι.',
                    style: TextStyle(color: Ink.muted),
                  ),
                  const Divider(height: 32),
                  OutlinedButton(
                    onPressed: _busy ? null : _report,
                    child: const Text('Αναφορά'),
                  ),
                ],
              ),
      ),
    );
  }
}

/// The report question: a reason, and room for the caller's own words.
class _ReportDialog extends StatefulWidget {
  const _ReportDialog();

  @override
  State<_ReportDialog> createState() => _ReportDialogState();
}

class _ReportDialogState extends State<_ReportDialog> {
  final TextEditingController _detail = TextEditingController();
  String? _reason;

  @override
  void dispose() {
    _detail.dispose();
    super.dispose();
  }

  bool get _ready => _reason != null;

  void _send() {
    final reason = _reason;
    if (reason == null) return;
    final detail = _detail.text.trim();
    // «Κάτι άλλο» without words says nothing worth reading.
    if (reason == 'other' && detail.isEmpty) return;
    Navigator.of(context).pop((id: reason, detail: detail));
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Τι συνέβη;'),
      content: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            for (final option in reportReasons)
              ListTile(
                contentPadding: EdgeInsets.zero,
                selected: _reason == option.id,
                leading: Icon(
                  _reason == option.id
                      ? Icons.radio_button_checked
                      : Icons.radio_button_unchecked,
                ),
                title: Text(option.label),
                onTap: () => setState(() => _reason = option.id),
              ),
            const SizedBox(height: 12),
            TextField(
              controller: _detail,
              maxLines: 3,
              minLines: 1,
              decoration: const InputDecoration(
                labelText: 'Πες μας λίγα παραπάνω (προαιρετικό)',
                hintText: 'Τι έγινε, με δικά σου λόγια',
              ),
              onChanged: (_) => setState(() {}),
            ),
            const SizedBox(height: 12),
            const Text(
              'Την αναφορά τη διαβάζει άνθρωπος. Ο άλλος δεν μαθαίνει ότι τον '
              'ανέφερες εσύ.',
              style: TextStyle(color: Ink.muted),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Άκυρο'),
        ),
        FilledButton(
          onPressed: _ready ? _send : null,
          child: const Text('Στείλε την αναφορά'),
        ),
      ],
    );
  }
}

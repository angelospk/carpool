import 'dart:async';

import 'package:flutter/material.dart' hide Ink;

import '../core/athens_time.dart';
import '../data/api.dart';
import '../data/models.dart';
import 'theme.dart';

/// The chat of one trip: what was said, and a box to say more.
///
/// It sits inside the trip screen's list, so it lays itself out in a column and
/// never takes a scroll view of its own.
class TripChat extends StatefulWidget {
  const TripChat({
    super.key,
    required this.api,
    required this.tripId,
    required this.myId,
    this.poll = const Duration(seconds: 10),
  });

  final Api api;
  final String tripId;

  /// The caller on this trip; their own messages carry no name.
  final String? myId;

  final Duration poll;

  @override
  State<TripChat> createState() => _TripChatState();
}

class _TripChatState extends State<TripChat> {
  final TextEditingController _draft = TextEditingController();
  List<ChatMessage> _messages = const [];
  bool _loaded = false;
  bool _sending = false;
  bool _loading = false;

  /// The last message a read returned. A message sent from here is not one:
  /// somebody else's, sent a moment before it, may not have been read yet.
  ChatMessage? _cursor;
  String? _error;
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    _load();
    if (widget.poll > Duration.zero) {
      _timer = Timer.periodic(widget.poll, (_) => _load());
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    _draft.dispose();
    super.dispose();
  }

  /// Reads the messages after the last one on screen, page by page, until the
  /// server has no more. A failure keeps what is already on screen.
  Future<void> _load() async {
    if (_loading) return;
    _loading = true;
    try {
      while (true) {
        final page = await widget.api.messages(widget.tripId, after: _cursor);
        if (!mounted) return;
        if (page.isNotEmpty) _cursor = page.last;
        setState(() {
          _messages = _merge(_messages, page);
          _loaded = true;
        });
        if (page.length < messagePage) break;
      }
    } catch (_) {
      if (!mounted) return;
      setState(() => _loaded = true);
    } finally {
      _loading = false;
    }
  }

  /// [old] plus whatever in [fetched] it does not have yet, in the server's
  /// order: by time, then id.
  List<ChatMessage> _merge(List<ChatMessage> old, List<ChatMessage> fetched) {
    final ids = {for (final message in old) message.id};
    final all = [...old, for (final message in fetched) if (!ids.contains(message.id)) message];
    all.sort((a, b) {
      final byTime = a.createdAt.compareTo(b.createdAt);
      return byTime != 0 ? byTime : a.id.compareTo(b.id);
    });
    return all;
  }

  Future<void> _send() async {
    final text = _draft.text.trim();
    if (text.isEmpty || _sending) return;
    setState(() {
      _sending = true;
      _error = null;
    });
    try {
      final sent = await widget.api.sendMessage(widget.tripId, text);
      if (!mounted) return;
      setState(() {
        _messages = _merge(_messages, [sent]);
        _draft.clear();
        _error = null;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _error = 'Το μήνυμα δεν στάλθηκε. Πάτα «Στείλε» ξανά.');
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final canSend = _draft.text.trim().isNotEmpty && !_sending;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const Text(
          'Συνομιλία',
          style: TextStyle(color: Ink.ink, fontSize: 18, fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 12),
        if (!_loaded)
          const Text('Φόρτωση…', style: TextStyle(color: Ink.muted, fontSize: 13)),
        for (final message in _messages) _bubble(message),
        const SizedBox(height: 4),
        Row(
          children: [
            Expanded(
              child: TextField(
                controller: _draft,
                enabled: !_sending,
                textInputAction: TextInputAction.send,
                decoration: const InputDecoration(hintText: 'Γράψε μήνυμα…'),
                onChanged: (_) => setState(() {}),
                onSubmitted: (_) => canSend ? _send() : null,
              ),
            ),
            const SizedBox(width: 12),
            FilledButton(
              onPressed: canSend ? _send : null,
              child: const Text('Στείλε'),
            ),
          ],
        ),
        if (_error != null) ...[
          const SizedBox(height: 8),
          Text(_error!, style: const TextStyle(color: Ink.danger, fontSize: 13)),
        ],
      ],
    );
  }

  Widget _bubble(ChatMessage message) {
    final mine = message.senderId == widget.myId;
    return Align(
      alignment: mine ? Alignment.centerRight : Alignment.centerLeft,
      child: Container(
        margin: const EdgeInsets.only(bottom: 8),
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
        constraints: const BoxConstraints(maxWidth: 280),
        decoration: BoxDecoration(
          color: mine ? Ink.accent : Colors.white,
          borderRadius: BorderRadius.circular(12),
          border: mine ? null : Border.all(color: Ink.line),
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (!mine) ...[
              Text(
                '${message.senderName} · ${athensHm(message.createdAt)}',
                style: const TextStyle(color: Ink.muted, fontSize: 12),
              ),
              const SizedBox(height: 2),
            ],
            Text(
              message.body,
              style: TextStyle(color: mine ? Ink.onAccent : Ink.ink, fontSize: 15),
            ),
          ],
        ),
      ),
    );
  }
}

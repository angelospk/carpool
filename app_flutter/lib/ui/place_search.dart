import 'dart:async';

import 'package:flutter/material.dart';

import '../data/api.dart';
import '../data/models.dart';
import 'messages.dart';

/// Asks for a settlement by typing its name, and returns the one picked.
///
/// The server is the only list of places there is: the app carries no dataset,
/// and a result that names an area is how somebody tells two villages with the
/// same name apart. Typing is debounced, because every keystroke would
/// otherwise be a request.
Future<Place?> pickPlace(
  BuildContext context,
  Api api, {
  String title = 'Πού;',
}) {
  return showDialog<Place>(
    context: context,
    builder: (context) => PlaceSearchDialog(api: api, title: title),
  );
}

class PlaceSearchDialog extends StatefulWidget {
  const PlaceSearchDialog({super.key, required this.api, required this.title});

  final Api api;
  final String title;

  @override
  State<PlaceSearchDialog> createState() => _PlaceSearchDialogState();
}

class _PlaceSearchDialogState extends State<PlaceSearchDialog> {
  final TextEditingController _controller = TextEditingController();
  Timer? _debounce;

  List<Place> _places = const [];
  bool _loading = false;
  String? _error;

  /// The query the shown list answers. An older answer that arrives late is
  /// dropped rather than replacing the list for what is now typed.
  String _answered = '';

  @override
  void dispose() {
    _debounce?.cancel();
    _controller.dispose();
    super.dispose();
  }

  void _onChanged(String value) {
    _debounce?.cancel();
    final query = value.trim();
    if (query.isEmpty) {
      setState(() {
        _places = const [];
        _loading = false;
        _error = null;
        _answered = '';
      });
      return;
    }
    _debounce = Timer(const Duration(milliseconds: 250), () => _search(query));
  }

  Future<void> _search(String query) async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final places = await widget.api.places(query);
      if (!mounted || _controller.text.trim() != query) return;
      setState(() {
        _places = places;
        _loading = false;
        _answered = query;
      });
    } catch (error) {
      if (!mounted || _controller.text.trim() != query) return;
      setState(() {
        _places = const [];
        _loading = false;
        _error = failureText(error);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Dialog.fullscreen(
      child: Scaffold(
        appBar: AppBar(
          title: Text(widget.title),
          leading: IconButton(
            icon: const Icon(Icons.close),
            tooltip: 'Άκυρο',
            onPressed: () => Navigator.of(context).pop(),
          ),
        ),
        body: Column(
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
              child: TextField(
                controller: _controller,
                autofocus: true,
                textInputAction: TextInputAction.search,
                onChanged: _onChanged,
                onSubmitted: (value) {
                  _debounce?.cancel();
                  if (value.trim().isNotEmpty) _search(value.trim());
                },
                decoration: const InputDecoration(
                  labelText: 'Πόλη ή χωριό',
                  hintText: 'π.χ. Φέρες',
                  prefixIcon: Icon(Icons.search),
                  border: OutlineInputBorder(),
                ),
              ),
            ),
            if (_loading) const LinearProgressIndicator(),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.all(16),
                child: Text(_error!, style: TextStyle(color: theme.colorScheme.error)),
              ),
            Expanded(
              child: ListView.builder(
                itemCount: _places.length,
                itemBuilder: (context, index) {
                  final place = _places[index];
                  return ListTile(
                    title: Text(place.name),
                    subtitle: place.area.isEmpty ? null : Text(place.area),
                    onTap: () => Navigator.of(context).pop(place),
                  );
                },
              ),
            ),
            if (!_loading && _error == null && _answered.isNotEmpty && _places.isEmpty)
              Padding(
                padding: const EdgeInsets.all(16),
                child: Text('Κανένα μέρος δεν ταιριάζει.', style: theme.textTheme.bodyMedium),
              ),
          ],
        ),
      ),
    );
  }
}

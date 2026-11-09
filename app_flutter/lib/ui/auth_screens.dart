import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../data/api.dart';
import '../data/models.dart';
import 'app_scope.dart';
import 'messages.dart';
import 'place_search.dart';

/// Greek mobile numbers, as somebody types them, and as the server wants them.
///
/// E.164 with the +30 country code and no spaces. Everything the person might
/// type — "+30 690 000 0000", "00306900000000", "690 000 0000" — becomes the
/// same string, so a number is one account rather than three.
String? normaliseGreekMobile(String typed) {
  var text = typed.replaceAll(RegExp(r'[^0-9+]'), '');
  if (text.startsWith('+')) {
    text = text.substring(1);
  }
  if (text.startsWith('0030')) text = text.substring(4);
  if (text.startsWith('30') && text.length == 12) text = text.substring(2);
  if (text.length != 10) return null;
  if (!text.startsWith('6') && !text.startsWith('9')) return null;
  return '+30$text';
}

bool looksLikeGreekMobile(String typed) => normaliseGreekMobile(typed) != null;

/// The number somebody typed, shown the way the server stores it.
String formatGreekMobile(String typed) {
  final normalised = normaliseGreekMobile(typed);
  if (normalised == null) return typed;
  final digits = normalised.substring(3);
  return '+30 ${digits.substring(0, 3)} ${digits.substring(3, 6)} ${digits.substring(6)}';
}

/// Signing in with a phone number.
class PhoneScreen extends StatefulWidget {
  const PhoneScreen({super.key, required this.api});

  final Api api;

  @override
  State<PhoneScreen> createState() => _PhoneScreenState();
}

class _PhoneScreenState extends State<PhoneScreen> {
  final TextEditingController _phone = TextEditingController(text: '+30 ');
  bool _busy = false;
  String? _error;

  bool get _valid => looksLikeGreekMobile(_phone.text);

  @override
  void dispose() {
    _phone.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    final phone = normaliseGreekMobile(_phone.text);
    if (phone == null || _busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final devCode = await widget.api.requestOtp(phone);
      if (!mounted) return;
      Navigator.of(context).push(MaterialPageRoute<void>(
        builder: (_) => OtpScreen(api: widget.api, phone: phone, devCode: devCode),
      ));
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
    return Scaffold(
      appBar: AppBar(title: const Text('Carpool')),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const SizedBox(height: 24),
              Text('Ο αριθμός σου', style: theme.textTheme.headlineSmall),
              const SizedBox(height: 8),
              Text(
                'Θα σου στείλουμε έναν κωδικό επιβεβαίωσης.',
                style: theme.textTheme.bodyMedium,
              ),
              const SizedBox(height: 24),
              TextField(
                controller: _phone,
                keyboardType: TextInputType.phone,
                autofocus: true,
                inputFormatters: [FilteringTextInputFormatter.allow(RegExp(r'[0-9+ ]'))],
                onChanged: (_) => setState(() {}),
                onSubmitted: (_) => _submit(),
                decoration: InputDecoration(
                  labelText: 'Κινητό',
                  hintText: '+30 690 000 0000',
                  border: const OutlineInputBorder(),
                  errorText: _phone.text.trim().length > 4 && !_valid ? 'Ελληνικό κινητό, 10 ψηφία' : null,
                ),
              ),
              if (_error != null) ...[
                const SizedBox(height: 16),
                Text(_error!, style: TextStyle(color: theme.colorScheme.error)),
              ],
              const SizedBox(height: 24),
              FilledButton(
                onPressed: _valid && !_busy ? _submit : null,
                child: _busy
                    ? const SizedBox(
                        height: 20,
                        width: 20,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Text('Συνέχεια'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Six digits, and then either a session or a ticket to finish signing up.
class OtpScreen extends StatefulWidget {
  const OtpScreen({super.key, required this.api, required this.phone, this.devCode});

  final Api api;
  final String phone;

  /// Present only when the server runs with DEV_MODE and hands the code back
  /// instead of texting it. Then the field starts full, and the tester is not
  /// sent to a log file they cannot reach.
  final String? devCode;

  @override
  State<OtpScreen> createState() => _OtpScreenState();
}

class _OtpScreenState extends State<OtpScreen> {
  final TextEditingController _code = TextEditingController();
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    if (widget.devCode != null) _code.text = widget.devCode!;
  }

  @override
  void dispose() {
    _code.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_code.text.length != 6 || _busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final result = await widget.api.verifyOtp(widget.phone, _code.text);
      if (!mounted) return;
      if (result.signupToken != null) {
        Navigator.of(context).push(MaterialPageRoute<void>(
          builder: (_) => SignupScreen(api: widget.api, signupToken: result.signupToken!),
        ));
        return;
      }
      // The tokens are already stored by verifyOtp; this only says where to go.
      AppScope.of(context).onSignedIn();
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = failureText(error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _resend() async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final devCode = await widget.api.requestOtp(widget.phone);
      if (!mounted) return;
      if (devCode != null) _code.text = devCode;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Σου στείλαμε νέο κωδικό.')),
      );
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
    return Scaffold(
      appBar: AppBar(title: const Text('Ο κωδικός')),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text('Έξι ψηφία, ισχύει για 5 λεπτά.', style: theme.textTheme.bodyMedium),
              const SizedBox(height: 6),
              Text('Στάλθηκε στο ${formatGreekMobile(widget.phone)}.', style: theme.textTheme.bodyMedium),
              const SizedBox(height: 24),
              TextField(
                controller: _code,
                autofocus: true,
                keyboardType: TextInputType.number,
                textAlign: TextAlign.center,
                maxLength: 6,
                inputFormatters: [
                  FilteringTextInputFormatter.digitsOnly,
                  LengthLimitingTextInputFormatter(6),
                ],
                style: const TextStyle(fontSize: 32, letterSpacing: 12),
                onChanged: (_) => setState(() {}),
                onSubmitted: (_) => _submit(),
                decoration: const InputDecoration(
                  counterText: '',
                  hintText: '······',
                  border: OutlineInputBorder(),
                ),
              ),
              const SizedBox(height: 12),
              Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  TextButton(
                    onPressed: _busy ? null : () => Navigator.of(context).pop(),
                    child: const Text('Άλλος αριθμός'),
                  ),
                  TextButton(
                    onPressed: _busy ? null : _resend,
                    child: const Text('Στείλε νέο κωδικό'),
                  ),
                ],
              ),
              if (_error != null) ...[
                const SizedBox(height: 12),
                Text(_error!, style: TextStyle(color: theme.colorScheme.error)),
              ],
              const SizedBox(height: 24),
              FilledButton(
                onPressed: _code.text.length == 6 && !_busy ? _submit : null,
                child: _busy
                    ? const SizedBox(
                        height: 20,
                        width: 20,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Text('Συνέχεια'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Joining, once the number is proved. The server decides whether this pilot
/// needs an invite code, and it decides whether the birthday is old enough.
class SignupScreen extends StatefulWidget {
  const SignupScreen({super.key, required this.api, required this.signupToken});

  final Api api;
  final String signupToken;

  @override
  State<SignupScreen> createState() => _SignupScreenState();
}

class _SignupScreenState extends State<SignupScreen> {
  final TextEditingController _name = TextEditingController();
  final TextEditingController _invite = TextEditingController();

  Place? _home;
  DateTime? _birthDate;
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _invite.dispose();
    super.dispose();
  }

  bool get _oldEnough {
    final born = _birthDate;
    if (born == null) return false;
    final now = DateTime.now();
    var age = now.year - born.year;
    if (now.month < born.month || (now.month == born.month && now.day < born.day)) age--;
    return age >= 18;
  }

  bool get _ready => _name.text.trim().isNotEmpty && _home != null && _oldEnough && !_busy;

  String get _isoBirthDate {
    final born = _birthDate!;
    final month = born.month.toString().padLeft(2, '0');
    final day = born.day.toString().padLeft(2, '0');
    return '${born.year}-$month-$day';
  }

  Future<void> _pickHome() async {
    final place = await pickPlace(context, widget.api, title: 'Η πόλη ή το χωριό σου');
    if (place == null || !mounted) return;
    setState(() => _home = place);
  }

  Future<void> _pickBirthDate() async {
    final now = DateTime.now();
    final picked = await showDatePicker(
      context: context,
      initialDate: _birthDate ?? DateTime(now.year - 25, now.month, now.day),
      firstDate: DateTime(1900),
      lastDate: now,
      helpText: 'Ημερομηνία γέννησης',
      initialEntryMode: DatePickerEntryMode.calendarOnly,
    );
    if (picked == null || !mounted) return;
    setState(() {
      _birthDate = picked;
      _error = null;
    });
  }

  Future<void> _submit() async {
    if (!_ready) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final code = _invite.text.trim().toUpperCase();
      await widget.api.completeSignup(
        signupToken: widget.signupToken,
        name: _name.text.trim(),
        homePlaceId: _home!.id,
        dateOfBirth: _isoBirthDate,
        inviteCode: code.isEmpty ? null : code,
      );
      if (!mounted) return;
      AppScope.of(context).onSignedIn();
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
    final born = _birthDate;
    return Scaffold(
      appBar: AppBar(title: const Text('Πες μας λίγα για σένα')),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              TextField(
                controller: _name,
                textCapitalization: TextCapitalization.words,
                onChanged: (_) => setState(() {}),
                decoration: const InputDecoration(
                  labelText: 'Το όνομά σου',
                  border: OutlineInputBorder(),
                ),
              ),
              const SizedBox(height: 16),
              InputDecorator(
                decoration: const InputDecoration(
                  labelText: 'Η πόλη ή το χωριό σου',
                  border: OutlineInputBorder(),
                ),
                child: Row(
                  children: [
                    Expanded(
                      child: Text(_home == null
                          ? 'Διάλεξε από τη λίστα'
                          : _home!.area.isEmpty
                              ? _home!.name
                              : '${_home!.name} · ${_home!.area}'),
                    ),
                    TextButton(
                      onPressed: _busy ? null : _pickHome,
                      child: Text(_home == null ? 'Αναζήτηση' : 'Αλλαγή'),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 6),
              Text(
                'Από πού ξεκινάς συνήθως. Το χρησιμοποιούμε για να βρούμε την περιοχή σου.',
                style: theme.textTheme.bodySmall,
              ),
              const SizedBox(height: 16),
              InputDecorator(
                decoration: InputDecoration(
                  labelText: 'Ημερομηνία γέννησης',
                  border: const OutlineInputBorder(),
                  errorText: born != null && !_oldEnough ? 'Πρέπει να είσαι 18 ετών.' : null,
                ),
                child: Row(
                  children: [
                    Expanded(
                      child: Text(born == null
                          ? 'Διάλεξε ημερομηνία'
                          : '${born.day}/${born.month}/${born.year}'),
                    ),
                    TextButton(
                      onPressed: _busy ? null : _pickBirthDate,
                      child: Text(born == null ? 'Διάλεξε' : 'Αλλαγή'),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 6),
              Text(
                'Χρειάζεται για να επιβεβαιώσουμε ότι είσαι ενήλικας. Δεν εμφανίζεται πουθενά.',
                style: theme.textTheme.bodySmall,
              ),
              const SizedBox(height: 16),
              TextField(
                controller: _invite,
                textCapitalization: TextCapitalization.characters,
                autocorrect: false,
                decoration: const InputDecoration(
                  labelText: 'Κωδικός πρόσκλησης (αν έχεις)',
                  border: OutlineInputBorder(),
                ),
              ),
              const SizedBox(height: 6),
              Text(
                'Αν σου έστειλαν μια διαδρομή, ο κωδικός είναι στη σελίδα της.',
                style: theme.textTheme.bodySmall,
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
                    : const Text('Συνέχεια'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

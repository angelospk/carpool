import 'package:timezone/data/latest.dart' as tzdata;
import 'package:timezone/timezone.dart' as tz;

bool _initialised = false;

/// Loads the time-zone database. Safe to call more than once.
void initAthensTime() {
  if (_initialised) return;
  tzdata.initializeTimeZones();
  _initialised = true;
}

tz.Location get _athens => tz.getLocation('Europe/Athens');

/// The Athens wall clock for an instant, as 'HH:mm'.
String athensHm(DateTime instant) {
  final local = tz.TZDateTime.from(instant.toUtc(), _athens);
  return '${local.hour.toString().padLeft(2, '0')}:${local.minute.toString().padLeft(2, '0')}';
}

const List<String> _weekdays = ['Δευ', 'Τρί', 'Τετ', 'Πέμ', 'Παρ', 'Σάβ', 'Κυρ'];

/// The Athens day for an instant, as 'Πέμ 30/7'.
String athensDay(DateTime instant) {
  final local = tz.TZDateTime.from(instant.toUtc(), _athens);
  final name = _weekdays[local.weekday - 1];
  return '$name ${local.day}/${local.month}';
}

/// The Athens wall clock now.
DateTime athensNow() => tz.TZDateTime.now(_athens);

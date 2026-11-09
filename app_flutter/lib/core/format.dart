import 'geo.dart';

/// The prime sign the Greek screens use for minutes: U+2032.
const String prime = '′';

/// One decimal place, with a Greek comma.
String _oneDecimal(double value) => value.toStringAsFixed(1).replaceAll('.', ',');

/// A detour as a driver reads it: minutes and kilometres, or the plain truth
/// that there is none.
String detourText(double min, double km) {
  final minutes = min.round();
  if (minutes == 0 && km < 0.05) return 'Χωρίς παράκαμψη';
  return '${detourMinutes(min)} · +${kmTextNumber(km)} χλμ';
}

/// Just the extra minutes, never less than one.
String detourMinutes(double min) {
  final minutes = min.round();
  return '+${minutes < 1 ? 1 : minutes}$prime';
}

/// When the car arrives: 'τώρα' when it is here, otherwise 'σε N′'.
String etaText(int minutes) => minutes <= 0 ? 'τώρα' : 'σε $minutes$prime';

/// Money as a suggested contribution: '3,50 €'.
String eur(double value) => '${value.toStringAsFixed(2).replaceAll('.', ',')} €';

/// A distance in kilometres, one decimal, with the ',0' dropped: '20 χλμ'.
String kmText(double km) => '${kmTextNumber(km)} χλμ';

/// The bare kilometres figure behind [kmText].
String kmTextNumber(double km) {
  final text = _oneDecimal(km);
  return text.endsWith(',0') ? text.substring(0, text.length - 2) : text;
}

/// A walk: metres under a kilometre, kilometres above, with the minutes.
String walkText(double meters) {
  if (meters >= 1000) {
    return '${kmTextNumber(meters / 1000)} χλμ · ≈${walkMinutes(meters)}$prime';
  }
  return '${meters.round()} μ · ≈${walkMinutes(meters)}$prime';
}

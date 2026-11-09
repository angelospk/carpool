import 'package:carpool/core/athens_time.dart';
import 'package:carpool/core/format.dart';
import 'package:carpool/core/geo.dart';
import 'package:carpool/core/polyline.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  setUpAll(initAthensTime);

  group('polyline6', () {
    test('decodes what OSRM sends, latitude first', () {
      final pts = decodePolyline6('_dh|lA_suep@?_ibE?_ibE');
      expect(pts.length, 3);
      expect(pts[0].lat, closeTo(40.85, 1e-6));
      expect(pts[0].lon, closeTo(25.80, 1e-6));
      expect(pts[2].lon, closeTo(26.00, 1e-6));
    });
    test('an empty string is no line', () => expect(decodePolyline6(''), isEmpty));
  });

  group('geo', () {
    test('a tenth of a degree east at 40.85° is about 8.4 km', () {
      final m = metersBetween(const LatLon(40.85, 25.80), const LatLon(40.85, 25.90));
      expect(m, closeTo(8411, 85));
    });
    test('walking is 80 m a minute, rounded up, at least one', () {
      expect(walkMinutes(280), 4);
      expect(walkMinutes(10), 1);
    });
  });

  group('Athens time', () {
    test('an instant is shown in Athens, not in the device zone', () {
      expect(athensHm(DateTime.utc(2026, 7, 30, 4, 30)), '07:30');
      expect(athensHm(DateTime.utc(2026, 1, 29, 4, 30)), '06:30');
    });
    test('the day names the weekday and the date', () {
      expect(athensDay(DateTime.utc(2026, 7, 30, 4, 30)), 'Πέμ 30/7');
    });
  });

  group('words', () {
    test('a passenger on the road costs no detour, and says so', () {
      expect(detourText(0, 0), 'Χωρίς παράκαμψη');
    });
    test('a detour is minutes and kilometres, Greek decimals', () {
      expect(detourText(6.4, 2.13), '+6′ · +2,1 χλμ');
      expect(detourMinutes(6.4), '+6′');
      expect(detourMinutes(0.2), '+1′');
    });
    test('arrival', () {
      expect(etaText(0), 'τώρα');
      expect(etaText(4), 'σε 4′');
    });
    test('kilometres drop a trailing ,0', () {
      expect(kmText(20.0), '20 χλμ');
      expect(kmText(2.13), '2,1 χλμ');
    });
    test('money is a suggested contribution with a comma', () {
      expect(eur(3.5), '3,50 €');
    });
    test('a walk is metres under a kilometre, kilometres above', () {
      expect(walkText(280), '280 μ · ≈4′');
      expect(walkText(1234), '1,2 χλμ · ≈16′');
    });
  });
}

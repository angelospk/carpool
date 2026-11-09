import 'package:carpool/core/athens_time.dart';
import 'package:carpool/core/geo.dart';
import 'package:carpool/data/models.dart';
import 'package:carpool/ui/ask_screen.dart';
import 'package:carpool/ui/home_screen.dart';
import 'package:carpool/ui/publish_screen.dart';
import 'package:carpool/ui/theme.dart';
import 'package:carpool/ui/trip_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../data/fixtures.dart';
import 'harness.dart';

Widget app(Widget home) => MaterialApp(theme: carpoolTheme(), home: home);

void main() {
  setUpAll(initAthensTime);

  testWidgets('the driver sees the detour first, then says yes', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({'GET /v1/asks/a1': askJson, 'POST /v1/asks/a1/accept': {'booking_id': 'b1'}});
    await tester.pumpWidget(app(AskScreen(api: f.api, askId: 'a1', mapBuilder: fakeMap)));
    await tester.pumpAndSettle();

    expect(find.text('+6′'), findsOneWidget);
    expect(find.textContaining('+2,1 χλμ'), findsOneWidget);
    expect(find.textContaining('Μαρία'), findsWidgets);
    expect(find.textContaining('3,50 €'), findsOneWidget);

    await tester.tap(find.text('Ναι, πάρ\' την'));
    await tester.pumpAndSettle();
    expect(f.calls, contains('POST /v1/asks/a1/accept'));
  });

  testWidgets('the passenger sees the car coming, the walk, and can share and confirm', (tester) async {
    await bigPhone(tester);
    String? shared;
    final f = fakeApi({
      'GET /v1/trips/occ1': tripJson,
      'GET /v1/trips/occ1/live': liveJson,
      'POST /v1/trips/occ1/share': {'token': 'tok', 'url': 'https://carpool.haroldpoi.dev/t/tok'},
      'POST /v1/trips/occ1/confirm': <String, Object>{},
    });
    await tester.pumpWidget(app(TripScreen(
      api: f.api,
      tripId: 'occ1',
      mapBuilder: fakeMap,
      locate: () async => const LatLon(40.853, 25.848),
      share: (text) async => shared = text,
      livePoll: const Duration(minutes: 5),
    )));
    await tester.pumpAndSettle();

    expect(find.textContaining('σε 8′'), findsOneWidget);
    expect(find.textContaining('Πλ. Δημαρχείου'), findsWidgets);
    expect(find.textContaining('≈5′'), findsOneWidget);
    expect(find.textContaining('Toyota Yaris'), findsOneWidget);
    expect(find.textContaining('ΕΒΡ 4521'), findsOneWidget);
    final map = tester.widget<Text>(find.byKey(const Key('map'))).data!;
    expect(map, contains('car'));
    expect(map, contains('pickup'));
    expect(map, contains('me'));
    expect(map, contains('walk'));
    expect(map, contains('route'));

    await tester.tap(find.text('Μοιράσου'));
    await tester.pumpAndSettle();
    expect(shared, contains('https://carpool.haroldpoi.dev/t/tok'));

    final confirm = find.text('Έγινε η διαδρομή');
    await tester.scrollUntilVisible(confirm, 200, scrollable: find.byType(Scrollable).last);
    await tester.tap(confirm);
    await tester.pumpAndSettle();
    expect(f.calls, contains('POST /v1/trips/occ1/confirm'));
  });

  testWidgets('a driver mid-drive keeps sharing after a restart, and Τέλος stops it', (tester) async {
    await bigPhone(tester);
    final started = <String>[];
    final ended = <String>[];
    final f = fakeApi({
      'GET /v1/trips/occ1': {
        ...tripJson,
        'role': 'driver',
        'drive_set_at': '2026-07-30T04:30:00Z',
        'drive_ended_at': null,
      },
      'GET /v1/trips/occ1/live': liveJson,
      'POST /v1/trips/occ1/drive/end': <String, Object>{},
    });
    await tester.pumpWidget(app(TripScreen(
      api: f.api,
      tripId: 'occ1',
      mapBuilder: fakeMap,
      locate: () async => null,
      livePoll: const Duration(minutes: 5),
      onStartDrive: (trip) async => started.add(trip.id),
      onEndDrive: (trip) async => ended.add(trip.id),
    )));
    await tester.pumpAndSettle();
    expect(started, ['occ1'], reason: 'the server says the drive is on, so this phone shares again');

    await tester.scrollUntilVisible(find.text('Τέλος διαδρομής'), 200,
        scrollable: find.byType(Scrollable).first);
    await tester.tap(find.text('Τέλος διαδρομής'));
    await tester.pumpAndSettle();
    expect(f.calls, contains('POST /v1/trips/occ1/drive/end'));
    expect(ended, ['occ1']);
  });

  testWidgets('publishing shows the roads and sends the one picked', (tester) async {
    await bigPhone(tester);
    PublishResult? done;
    final f = fakeApi({
      'POST /v1/routes/preview': previewJson,
      'POST /v1/routes': {'template_id': 't9', 'route_km': 26.0, 'ends_at_port': false, 'first_occurrence_id': 'o9'},
    });
    await tester.pumpWidget(app(PublishScreen(
      api: f.api,
      mapBuilder: fakeMap,
      today: DateTime(2026, 7, 29),
      initialOrigin: const Place(id: 'p1', name: 'Αλεξανδρούπολη', area: '', lat: 40.85, lon: 25.8),
      initialDestination: const Place(id: 'p2', name: 'Φέρες', area: '', lat: 40.85, lon: 26.0),
      onPublished: (r) => done = r,
    )));
    await tester.pumpAndSettle();

    expect(find.text('20′ · 20 χλμ'), findsOneWidget);
    expect(find.text('25′ · 26 χλμ'), findsOneWidget);
    await tester.tap(find.text('25′ · 26 χλμ'));
    await tester.pumpAndSettle();
    final map = tester.widget<Text>(find.byKey(const Key('map'))).data!;
    expect(map, contains('route'));
    expect(map, contains('alternative'));

    final publish = find.text('Δημοσίευση');
    await tester.scrollUntilVisible(publish, 200, scrollable: find.byType(Scrollable).last);
    await tester.tap(publish);
    await tester.pumpAndSettle();

    final body = f.bodies['POST /v1/routes'] as Map<String, dynamic>;
    expect(body['via'], [25.9, 40.95]);
    expect(body['oneoff_date'], '2026-07-30');
    expect(body['depart_earliest'], '07:30');
    expect(body['seats'], 3);
    expect(done?.templateId, 't9');
  });

  testWidgets('home shows the next trip over the map', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/me': {'id': 'u1', 'phone': '+30690', 'name': 'Ελένη'},
      'GET /v1/trips/upcoming': {
        'trips': [
          {
            'id': 'occ1', 'role': 'passenger', 'depart_earliest': '2026-07-30T04:30:00Z',
            'depart_latest': '2026-07-30T05:00:00Z', 'status': 'scheduled', 'route_km': 17.0,
            'origin_label': 'Αλεξανδρούπολη', 'destination_label': 'Φέρες', 'price_eur': 3.5, 'seats_free': 2,
          }
        ]
      },
      'GET /v1/asks/pending': {'asks': <Object>[]},
      'GET /v1/requests/mine': {'requests': <Object>[]},
      'GET /v1/live/map': {'trips': <Object>[]},
    });
    await tester.pumpWidget(app(HomeScreen(api: f.api, mapBuilder: fakeMap)));
    await tester.pumpAndSettle();
    expect(find.textContaining('Ελένη'), findsWidgets);
    expect(find.text('07:30'), findsOneWidget);
    expect(find.textContaining('Αλεξανδρούπολη → Φέρες'), findsOneWidget);
    expect(find.text('Ψάξε διαδρομή'), findsOneWidget);
  });
}

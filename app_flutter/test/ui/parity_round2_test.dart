// Round two of the parity screens: what the emulator run and the review found.
// Requests still out, a cancelled trip, a home screen that goes stale.

import 'dart:async';

import 'package:carpool/core/athens_time.dart';
import 'package:carpool/data/api.dart';
import 'package:carpool/ui/home_screen.dart';
import 'package:carpool/ui/my_routes_screen.dart';
import 'package:carpool/ui/theme.dart';
import 'package:carpool/ui/trip_chat.dart';
import 'package:carpool/ui/trip_screen.dart';
import 'package:carpool/ui/user_profile_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../data/fixtures.dart';
import 'harness.dart';

Widget app(Widget home) => MaterialApp(theme: carpoolTheme(), home: home);

Finder inDialog(String text) =>
    find.descendant(of: find.byType(AlertDialog), matching: find.text(text));

Future<void> scrollTo(WidgetTester tester, Finder finder) async {
  await tester.scrollUntilVisible(finder, 200, scrollable: find.byType(Scrollable).first);
  await tester.pumpAndSettle();
}

TripScreen tripScreen(Api api) => TripScreen(
      api: api,
      tripId: 'occ1',
      mapBuilder: fakeMap,
      locate: () async => null,
      livePoll: const Duration(minutes: 5),
      chatPoll: const Duration(minutes: 5),
      onStartDrive: (_) async {},
    );

int count(List<String> calls, String key) => calls.where((c) => c == key).length;

void main() {
  setUpAll(initAthensTime);

  // -------------------------------------------------------------- trip screen

  testWidgets('a cancelled trip offers nothing to do but says it is cancelled', (tester) async {
    await bigPhone(tester);
    for (final role in ['driver', 'passenger']) {
      final f = fakeApi({
        'GET /v1/trips/occ1': {...tripJson, 'role': role, 'status': 'cancelled'},
        'GET /v1/trips/occ1/live': liveJson,
      });
      await tester.pumpWidget(app(tripScreen(f.api)));
      await tester.pumpAndSettle();
      expect(find.text('Ακυρώθηκε', skipOffstage: false), findsOneWidget, reason: role);
      for (final action in ['Φεύγω τώρα', 'Έγινε η διαδρομή', 'Ακύρωση διαδρομής', 'Δεν θα έρθω', 'Τέλος διαδρομής']) {
        expect(find.text(action, skipOffstage: false), findsNothing, reason: '$role: $action');
      }
      await tester.pumpWidget(const SizedBox());
    }
  });

  testWidgets('one passenger to tell reads in the singular', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/trips/occ1': {...tripJson, 'role': 'driver'},
      'GET /v1/trips/occ1/live': liveJson,
    });
    await tester.pumpWidget(app(tripScreen(f.api)));
    await tester.pumpAndSettle();
    await scrollTo(tester, find.text('Ακύρωση διαδρομής'));
    await tester.tap(find.text('Ακύρωση διαδρομής'));
    await tester.pumpAndSettle();
    expect(find.text('Η διαδρομή δεν θα γίνει. Θα ειδοποιηθεί 1 επιβάτης.'), findsOneWidget);
  });

  testWidgets('two passengers to tell read in the plural', (tester) async {
    await bigPhone(tester);
    final passenger = (tripJson['passengers'] as List).first as Map;
    final f = fakeApi({
      'GET /v1/trips/occ1': {
        ...tripJson,
        'role': 'driver',
        'passengers': [passenger, {...passenger, 'id': 'p2', 'name': 'Σοφία'}],
      },
      'GET /v1/trips/occ1/live': liveJson,
    });
    await tester.pumpWidget(app(tripScreen(f.api)));
    await tester.pumpAndSettle();
    await scrollTo(tester, find.text('Ακύρωση διαδρομής'));
    await tester.tap(find.text('Ακύρωση διαδρομής'));
    await tester.pumpAndSettle();
    expect(find.text('Η διαδρομή δεν θα γίνει. Θα ειδοποιηθούν 2 επιβάτες.'), findsOneWidget);
  });

  testWidgets('confirming reads the trip again, so a rating can appear', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/trips/occ1': tripJson,
      'GET /v1/trips/occ1/live': liveJson,
      'POST /v1/trips/occ1/confirm': <String, Object>{},
    });
    await tester.pumpWidget(app(tripScreen(f.api)));
    await tester.pumpAndSettle();
    await scrollTo(tester, find.text('Έγινε η διαδρομή'));
    await tester.tap(find.text('Έγινε η διαδρομή'));
    await tester.pumpAndSettle();
    expect(count(f.calls, 'GET /v1/trips/occ1'), 2);
  });

  // --------------------------------------------------------------------- chat

  testWidgets('the message box is locked while a message is on its way', (tester) async {
    await bigPhone(tester);
    final hold = Completer<void>();
    final f = fakeApi({
      'GET /v1/trips/occ1/messages': {'messages': <Object>[]},
      'POST /v1/trips/occ1/messages': messageJson('m1', 'p1', 'Ελένη', 'Έρχομαι'),
    }, holds: {'POST /v1/trips/occ1/messages': hold});
    await tester.pumpWidget(app(Scaffold(
      body: TripChat(api: f.api, tripId: 'occ1', myId: 'p1', poll: const Duration(minutes: 5)),
    )));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'Έρχομαι');
    await tester.pump();
    await tester.tap(find.widgetWithText(FilledButton, 'Στείλε'));
    await tester.pump();
    expect(tester.widget<TextField>(find.byType(TextField)).enabled, isFalse);
    hold.complete();
    await tester.pumpAndSettle();
    expect(tester.widget<TextField>(find.byType(TextField)).enabled, isNot(false));
  });

  // ------------------------------------------------------------- user profile

  testWidgets('when the block list will not load, no block button guesses', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({'GET /v1/me/blocks': const Fail(500, 'internal')});
    await tester.pumpWidget(app(UserProfileScreen(api: f.api, userId: 'u2', name: 'Κώστας')));
    await tester.pumpAndSettle();
    expect(find.text('Αποκλεισμός'), findsNothing);
    expect(find.text('Άρση αποκλεισμού'), findsNothing);
    expect(find.text('Δοκίμασε ξανά'), findsOneWidget);
  });

  testWidgets('a report on its way cannot be sent twice', (tester) async {
    await bigPhone(tester);
    final hold = Completer<void>();
    final f = fakeApi({
      'GET /v1/me/blocks': {'blocked': <Object>[]},
      'POST /v1/users/u2/report': const NoContent(),
    }, holds: {'POST /v1/users/u2/report': hold});
    await tester.pumpWidget(app(UserProfileScreen(api: f.api, userId: 'u2', name: 'Κώστας')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Αναφορά'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Δεν εμφανίστηκε'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Στείλε την αναφορά'));
    await tester.pumpAndSettle();
    final report = find.widgetWithText(OutlinedButton, 'Αναφορά');
    expect(tester.widget<OutlinedButton>(report).onPressed, isNull);
    hold.complete();
    await tester.pumpAndSettle();
    expect(tester.widget<OutlinedButton>(report).onPressed, isNotNull);
  });

  // ---------------------------------------------------------------- my routes

  testWidgets('the nudge switch waits for its answer before it moves again', (tester) async {
    await bigPhone(tester);
    final hold = Completer<void>();
    final f = fakeApi({
      'GET /v1/me/routes': {'routes': [routeJson]},
      'PUT /v1/me/routes/r1/nudge': <String, Object>{},
    }, holds: {'PUT /v1/me/routes/r1/nudge': hold});
    await tester.pumpWidget(app(MyRoutesScreen(api: f.api)));
    await tester.pumpAndSettle();
    await tester.tap(find.byType(Switch));
    await tester.pump();
    await tester.tap(find.byType(Switch));
    await tester.pump();
    expect(count(f.calls, 'PUT /v1/me/routes/r1/nudge'), 1);
    hold.complete();
    await tester.pumpAndSettle();
    expect(tester.widget<Switch>(find.byType(Switch)).value, isTrue);
  });

  testWidgets('a one-off route shows its day the way the home screen does', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/me/routes': {
        'routes': [
          {...routeJson, 'kind': 'oneoff', 'weekdays': <int>[], 'oneoff_date': '2026-11-10'},
        ],
      },
    });
    await tester.pumpWidget(app(MyRoutesScreen(api: f.api)));
    await tester.pumpAndSettle();
    expect(find.text('Τρί 10/11'), findsOneWidget);
    expect(find.textContaining('2026-11-10'), findsNothing);
  });

  // --------------------------------------------------------------------- home

  testWidgets('home reads its lists again when a screen on top closes', (tester) async {
    await bigPhone(tester);
    final observer = RouteObserver<ModalRoute<void>>();
    final navigator = GlobalKey<NavigatorState>();
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
    await tester.pumpWidget(MaterialApp(
      theme: carpoolTheme(),
      navigatorKey: navigator,
      navigatorObservers: [observer],
      home: HomeScreen(
        api: f.api,
        mapBuilder: fakeMap,
        routeObserver: observer,
        onOpenTrip: (_) => navigator.currentState!.push(MaterialPageRoute<void>(
          builder: (_) => Scaffold(appBar: AppBar(title: const Text('trip'))),
        )),
      ),
    ));
    await tester.pumpAndSettle();
    expect(count(f.calls, 'GET /v1/trips/upcoming'), 1);
    await tester.tap(find.text('07:30'));
    await tester.pumpAndSettle();
    await tester.pageBack();
    await tester.pumpAndSettle();
    expect(count(f.calls, 'GET /v1/trips/upcoming'), 2);
  });
  testWidgets('home lists past rides, and one opens its trip to rate it', (tester) async {
    await bigPhone(tester);
    String? opened;
    final f = fakeApi({
      'GET /v1/me': {'id': 'p1', 'phone': '+30690', 'name': 'Ελένη'},
      'GET /v1/trips/upcoming': {'trips': <Object>[]},
      'GET /v1/trips/past': {
        'trips': [
          {
            'id': 'occ9', 'booking_id': 'b9', 'role': 'passenger',
            'depart_earliest': '2026-07-28T04:30:00Z', 'depart_latest': '2026-07-28T05:00:00Z',
            'status': 'completed', 'route_km': 17.0, 'origin_label': 'Αλεξανδρούπολη',
            'destination_label': 'Φέρες', 'price_eur': 3.5, 'seats_free': 2, 'seats': 1,
            'driver_name': 'Νίκος',
          }
        ]
      },
      'GET /v1/asks/pending': {'asks': <Object>[]},
      'GET /v1/requests/mine': {'requests': <Object>[]},
      'GET /v1/live/map': {'trips': <Object>[]},
    });
    await tester.pumpWidget(app(HomeScreen(api: f.api, mapBuilder: fakeMap, onOpenTrip: (id) => opened = id)));
    await tester.pumpAndSettle();
    await scrollTo(tester, find.text('Παλιότερες διαδρομές'));
    expect(find.text('Αλεξανδρούπολη → Φέρες'), findsOneWidget);
    expect(find.textContaining('Ολοκληρώθηκε'), findsOneWidget);
    expect(find.textContaining('Νίκος'), findsOneWidget);
    await tester.tap(find.text('Αλεξανδρούπολη → Φέρες'));
    expect(opened, 'occ9');
  });
}

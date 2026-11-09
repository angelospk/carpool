// Screens the Expo app has and the Flutter app gained for parity: account
// deletion, blocking and reports, chat, ratings, cancelling, the driver's routes.

import 'dart:convert';

import 'package:carpool/core/athens_time.dart';
import 'package:carpool/data/api.dart';
import 'package:carpool/ui/blocked_screen.dart';
import 'package:carpool/ui/delete_account_screen.dart';
import 'package:carpool/ui/my_routes_screen.dart';
import 'package:carpool/ui/profile_screen.dart';
import 'package:carpool/ui/theme.dart';
import 'package:carpool/ui/trip_chat.dart';
import 'package:carpool/ui/trip_screen.dart';
import 'package:carpool/ui/user_profile_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import '../data/fixtures.dart';
import 'harness.dart';

Widget app(Widget home) => MaterialApp(theme: carpoolTheme(), home: home);

/// A first screen with one button that pushes [screen], so a pop shows.
Widget pushed(Widget screen) => MaterialApp(
      theme: carpoolTheme(),
      home: Builder(
        builder: (context) => Scaffold(
          body: Center(
            child: TextButton(
              onPressed: () => Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => screen)),
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );

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
    );

const me = {'id': 'p1', 'phone': '+306900000031', 'name': 'Ελένη'};

void main() {
  setUpAll(initAthensTime);
  chatPagingTests();

  // ------------------------------------------------------------ delete account

  testWidgets('the profile deletes the account after a warning and an SMS code', (tester) async {
    await bigPhone(tester);
    var signedOut = 0;
    final f = fakeApi({
      'GET /v1/me': me,
      'POST /v1/me/delete-code': {'dev_code': '123456'},
      'DELETE /v1/me': const NoContent(),
    });
    await tester.pumpWidget(app(ProfileScreen(api: f.api, onSignOut: () async => signedOut++)));
    await tester.pumpAndSettle();

    await scrollTo(tester, find.text('Διαγραφή λογαριασμού'));
    await tester.tap(find.text('Διαγραφή λογαριασμού'));
    await tester.pumpAndSettle();
    expect(
      find.text('Ο λογαριασμός σου θα διαγραφεί για πάντα και δεν γίνεται επαναφορά. '
          'Θα σου στείλουμε κωδικό με SMS για να το επιβεβαιώσεις.'),
      findsOneWidget,
    );
    await tester.tap(inDialog('Διαγραφή λογαριασμού'));
    await tester.pumpAndSettle();

    expect(f.calls, contains('POST /v1/me/delete-code'));
    expect(find.text('Σου στείλαμε κωδικό με SMS'), findsOneWidget);
    expect(find.widgetWithText(TextField, '123456'), findsOneWidget, reason: 'dev mode fills the code in');

    await tester.tap(find.widgetWithText(FilledButton, 'Διαγραφή λογαριασμού'));
    await tester.pumpAndSettle();
    expect(f.bodies['DELETE /v1/me'], {'code': '123456'});
    expect(signedOut, 1);
  });

  testWidgets('a wrong delete code says so and keeps the account', (tester) async {
    await bigPhone(tester);
    var deleted = 0;
    final f = fakeApi({
      'POST /v1/me/delete-code': const NoContent(),
      'DELETE /v1/me': const Fail(403, 'bad_code'),
    });
    await tester.pumpWidget(app(DeleteAccountScreen(api: f.api, onDeleted: () async => deleted++)));
    await tester.pumpAndSettle();

    final button = find.widgetWithText(FilledButton, 'Διαγραφή λογαριασμού');
    await tester.enterText(find.byType(TextField), '12345');
    await tester.pump();
    expect(tester.widget<FilledButton>(button).onPressed, isNull, reason: 'five digits are not a code');

    await tester.enterText(find.byType(TextField), '000000');
    await tester.pump();
    await tester.tap(button);
    await tester.pumpAndSettle();
    expect(find.text('Λάθος ή ληγμένος κωδικός.'), findsOneWidget);
    expect(deleted, 0);
  });

  testWidgets('a refused delete code says why instead of «sent»', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({'POST /v1/me/delete-code': const Fail(429, 'rate_limited')});
    await tester.pumpWidget(app(DeleteAccountScreen(api: f.api, onDeleted: () async {})));
    await tester.pumpAndSettle();
    expect(find.text('Πολλές προσπάθειες. Δοκίμασε σε λίγο.'), findsOneWidget);
    expect(find.text('Σου στείλαμε κωδικό με SMS'), findsNothing);
  });

  // ------------------------------------------------------- block and report

  testWidgets('the passenger opens the driver from the trip and blocks them', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/trips/occ1': tripJson,
      'GET /v1/trips/occ1/live': liveJson,
      'GET /v1/trips/occ1/messages': {'messages': <Object>[]},
      'GET /v1/me/blocks': {'blocked': <Object>[]},
      'POST /v1/users/d1/block': const NoContent(),
      'DELETE /v1/users/d1/block': const NoContent(),
    });
    await tester.pumpWidget(app(tripScreen(f.api)));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('person-d1')));
    await tester.pumpAndSettle();
    expect(find.text('Προφίλ'), findsOneWidget);
    expect(find.text('Νίκος'), findsWidgets);

    await tester.tap(find.text('Αποκλεισμός'));
    await tester.pumpAndSettle();
    expect(f.calls, contains('POST /v1/users/d1/block'));
    expect(find.text('Ο χρήστης αποκλείστηκε'), findsOneWidget);

    await tester.tap(find.text('Άρση αποκλεισμού'));
    await tester.pumpAndSettle();
    expect(f.calls, contains('DELETE /v1/users/d1/block'));
    expect(find.text('Ο αποκλεισμός αναιρέθηκε'), findsOneWidget);
  });

  testWidgets('the driver opens a passenger from the trip', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/trips/occ1': {...tripJson, 'role': 'driver'},
      'GET /v1/trips/occ1/live': liveJson,
      'GET /v1/me/blocks': {'blocked': <Object>[]},
    });
    await tester.pumpWidget(app(tripScreen(f.api)));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('person-p1')));
    await tester.pumpAndSettle();
    expect(find.text('Προφίλ'), findsOneWidget);
    expect(find.text('Ελένη'), findsWidgets);
  });

  testWidgets('somebody already blocked shows «Άρση αποκλεισμού»', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/me/blocks': {
        'blocked': [
          {'id': 'u2', 'name': 'Κώστας', 'photo_url': null},
        ],
      },
    });
    await tester.pumpWidget(app(UserProfileScreen(api: f.api, userId: 'u2', name: 'Κώστας')));
    await tester.pumpAndSettle();
    expect(find.text('Άρση αποκλεισμού'), findsOneWidget);
    expect(find.text('Αποκλεισμός'), findsNothing);
  });

  testWidgets('a report names a reason and adds the detail for «Κάτι άλλο»', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/me/blocks': {'blocked': <Object>[]},
      'POST /v1/users/u2/report': const NoContent(),
    });
    await tester.pumpWidget(app(UserProfileScreen(api: f.api, userId: 'u2', name: 'Κώστας')));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Αναφορά'));
    await tester.pumpAndSettle();
    expect(find.text('Τι συνέβη;'), findsOneWidget);
    expect(find.text('Επικίνδυνη οδήγηση'), findsOneWidget);
    await tester.tap(find.text('Κάτι άλλο'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'Άργησε μία ώρα');
    await tester.tap(find.text('Στείλε την αναφορά'));
    await tester.pumpAndSettle();

    expect(f.bodies['POST /v1/users/u2/report'], {'reason': 'other: Άργησε μία ώρα'});
    expect(find.text('Η αναφορά στάλθηκε'), findsOneWidget);
  });

  testWidgets('too many reports today says the earlier ones are being read', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/me/blocks': {'blocked': <Object>[]},
      'POST /v1/users/u2/report': const Fail(429, 'report_rate_limited'),
    });
    await tester.pumpWidget(app(UserProfileScreen(api: f.api, userId: 'u2', name: 'Κώστας')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Αναφορά'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Δεν εμφανίστηκε'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Στείλε την αναφορά'));
    await tester.pumpAndSettle();
    expect(f.bodies['POST /v1/users/u2/report'], {'reason': 'no_show'});
    expect(find.text('Έχεις στείλει πολλές αναφορές σήμερα. Αυτές που έστειλες εξετάζονται.'), findsOneWidget);
  });

  testWidgets('the blocked list lifts a block and reads the list again', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/me/blocks': {
        'blocked': [
          {'id': 'u2', 'name': 'Κώστας', 'photo_url': null},
        ],
      },
      'DELETE /v1/users/u2/block': const NoContent(),
    });
    await tester.pumpWidget(app(BlockedScreen(api: f.api)));
    await tester.pumpAndSettle();
    expect(find.text('Κώστας'), findsOneWidget);
    await tester.tap(find.text('Άρση αποκλεισμού'));
    await tester.pumpAndSettle();
    expect(f.calls, ['GET /v1/me/blocks', 'DELETE /v1/users/u2/block', 'GET /v1/me/blocks']);
  });

  testWidgets('an empty blocked list says so', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({'GET /v1/me/blocks': {'blocked': <Object>[]}});
    await tester.pumpWidget(app(BlockedScreen(api: f.api)));
    await tester.pumpAndSettle();
    expect(find.text('Δεν έχεις αποκλείσει κανέναν.'), findsOneWidget);
  });

  testWidgets('the profile opens the blocked list and the routes', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/me': me,
      'GET /v1/me/blocks': {'blocked': <Object>[]},
      'GET /v1/me/routes': {'routes': <Object>[]},
    });
    await tester.pumpWidget(app(ProfileScreen(api: f.api)));
    await tester.pumpAndSettle();
    await scrollTo(tester, find.text('Αποκλεισμένοι'));
    await tester.tap(find.text('Αποκλεισμένοι'));
    await tester.pumpAndSettle();
    expect(find.text('Δεν έχεις αποκλείσει κανέναν.'), findsOneWidget);
    await tester.pageBack();
    await tester.pumpAndSettle();
    await scrollTo(tester, find.text('Οι διαδρομές μου'));
    await tester.tap(find.text('Οι διαδρομές μου'));
    await tester.pumpAndSettle();
    expect(find.text('Δεν έχεις δημοσιεύσει διαδρομές'), findsOneWidget);
  });

  // --------------------------------------------------------------------- chat

  testWidgets('chat shows who wrote what and sends a message', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/trips/occ1/messages': {
        'messages': [
          messageJson('m1', 'd1', 'Νίκος', 'Καλημέρα, στις 7:30 εκεί'),
          messageJson('m2', 'p1', 'Ελένη', 'Εντάξει'),
        ],
      },
      'POST /v1/trips/occ1/messages': messageJson('m3', 'p1', 'Ελένη', 'Έρχομαι'),
    });
    await tester.pumpWidget(app(Scaffold(
      body: TripChat(api: f.api, tripId: 'occ1', myId: 'p1', poll: const Duration(minutes: 5)),
    )));
    await tester.pumpAndSettle();

    expect(find.text('Συνομιλία'), findsOneWidget);
    expect(find.text('Καλημέρα, στις 7:30 εκεί'), findsOneWidget);
    expect(find.text('Εντάξει'), findsOneWidget);
    expect(find.textContaining('Νίκος'), findsOneWidget, reason: 'the other person\'s name is on their message');
    expect(find.textContaining('Ελένη'), findsNothing, reason: 'my own messages carry no name');

    final send = find.widgetWithText(FilledButton, 'Στείλε');
    expect(tester.widget<FilledButton>(send).onPressed, isNull, reason: 'nothing to send yet');
    await tester.enterText(find.byType(TextField), '   ');
    await tester.pump();
    expect(tester.widget<FilledButton>(send).onPressed, isNull, reason: 'blanks are nothing');

    await tester.enterText(find.byType(TextField), ' Έρχομαι ');
    await tester.pump();
    await tester.tap(send);
    await tester.pumpAndSettle();
    expect(f.bodies['POST /v1/trips/occ1/messages'], {'body': 'Έρχομαι'});
    expect(find.text('Έρχομαι'), findsOneWidget);
    expect(tester.widget<TextField>(find.byType(TextField)).controller!.text, isEmpty);
  });

  testWidgets('a message that did not go keeps the draft and says so', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/trips/occ1/messages': {'messages': <Object>[]},
      'POST /v1/trips/occ1/messages': const Fail(500, 'internal'),
    });
    await tester.pumpWidget(app(Scaffold(
      body: TripChat(api: f.api, tripId: 'occ1', myId: 'p1', poll: const Duration(minutes: 5)),
    )));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'Έρχομαι');
    await tester.pump();
    await tester.tap(find.widgetWithText(FilledButton, 'Στείλε'));
    await tester.pumpAndSettle();
    expect(find.text('Το μήνυμα δεν στάλθηκε. Πάτα «Στείλε» ξανά.'), findsOneWidget);
    expect(tester.widget<TextField>(find.byType(TextField)).controller!.text, 'Έρχομαι');
  });

  testWidgets('chat polls for new messages', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({'GET /v1/trips/occ1/messages': {'messages': <Object>[]}});
    await tester.pumpWidget(app(Scaffold(
      body: TripChat(api: f.api, tripId: 'occ1', myId: 'p1', poll: const Duration(seconds: 10)),
    )));
    await tester.pumpAndSettle();
    expect(f.calls.where((c) => c == 'GET /v1/trips/occ1/messages').length, 1);
    await tester.pump(const Duration(seconds: 10));
    await tester.pumpAndSettle();
    expect(f.calls.where((c) => c == 'GET /v1/trips/occ1/messages').length, 2);
  });

  testWidgets('the trip screen carries the chat', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/trips/occ1': tripJson,
      'GET /v1/trips/occ1/live': liveJson,
      'GET /v1/trips/occ1/messages': {
        'messages': [messageJson('m1', 'd1', 'Νίκος', 'Καλημέρα')],
      },
    });
    await tester.pumpWidget(app(tripScreen(f.api)));
    await tester.pumpAndSettle();
    await scrollTo(tester, find.text('Καλημέρα'));
    expect(find.text('Συνομιλία'), findsOneWidget);
  });

  // ------------------------------------------------------------------ ratings

  testWidgets('after the trip the passenger rates the driver', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/trips/occ1': {
        ...tripJson,
        'status': 'completed',
        'confirmed_by_me': true,
        'driver': {...(tripJson['driver'] as Map), 'can_rate': true, 'rated': false},
      },
      'GET /v1/trips/occ1/live': liveJson,
      'POST /v1/trips/occ1/rate': <String, Object>{},
    });
    await tester.pumpWidget(app(tripScreen(f.api)));
    await tester.pumpAndSettle();

    await scrollTo(tester, find.text('Βαθμολόγησε'));
    await scrollTo(tester, find.byKey(const Key('star-d1-4')));
    await tester.tap(find.byKey(const Key('star-d1-4')));
    await tester.pumpAndSettle();
    await scrollTo(tester, find.text('Στείλε βαθμολογία'));
    await tester.tap(find.text('Στείλε βαθμολογία'));
    await tester.pumpAndSettle();
    expect(f.bodies['POST /v1/trips/occ1/rate'], {'stars': 4, 'ratee_id': 'd1'});
    expect(f.calls.where((c) => c == 'GET /v1/trips/occ1').length, 2, reason: 'the trip is read again');
  });

  testWidgets('a rated driver shows «Βαθμολογήθηκε», not the stars', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/trips/occ1': {
        ...tripJson,
        'status': 'completed',
        'driver': {...(tripJson['driver'] as Map), 'can_rate': false, 'rated': true},
      },
      'GET /v1/trips/occ1/live': liveJson,
    });
    await tester.pumpWidget(app(tripScreen(f.api)));
    await tester.pumpAndSettle();
    await scrollTo(tester, find.text('Βαθμολογήθηκε'));
    expect(find.byKey(const Key('star-d1-4')), findsNothing);
  });

  testWidgets('nobody to rate, no rating card', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({'GET /v1/trips/occ1': tripJson, 'GET /v1/trips/occ1/live': liveJson});
    await tester.pumpWidget(app(tripScreen(f.api)));
    await tester.pumpAndSettle();
    expect(find.text('Βαθμολόγησε', skipOffstage: false), findsNothing);
  });

  testWidgets('confirming before the start says when it will work', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/trips/occ1': tripJson,
      'GET /v1/trips/occ1/live': liveJson,
      'POST /v1/trips/occ1/confirm': const Fail(409, 'not_departed'),
    });
    await tester.pumpWidget(app(tripScreen(f.api)));
    await tester.pumpAndSettle();
    await scrollTo(tester, find.text('Έγινε η διαδρομή'));
    await tester.tap(find.text('Έγινε η διαδρομή'));
    await tester.pumpAndSettle();
    expect(find.text('Θα μπορείς μόλις ξεκινήσει η διαδρομή'), findsOneWidget);
  });

  // ------------------------------------------------------------------ cancel

  testWidgets('the passenger says «Δεν θα έρθω» and leaves the trip', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/trips/occ1': tripJson,
      'GET /v1/trips/occ1/live': liveJson,
      'DELETE /v1/bookings/b1': const NoContent(),
    });
    await tester.pumpWidget(pushed(tripScreen(f.api)));
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    await scrollTo(tester, find.text('Δεν θα έρθω'));
    await tester.tap(find.text('Δεν θα έρθω'));
    await tester.pumpAndSettle();
    expect(find.text('Η θέση σου επιστρέφει και ο/η Νίκος θα ειδοποιηθεί.'), findsOneWidget);
    await tester.tap(inDialog('Δεν θα έρθω'));
    await tester.pumpAndSettle();
    expect(f.calls, contains('DELETE /v1/bookings/b1'));
    expect(find.text('open'), findsOneWidget, reason: 'the trip screen closed');
  });

  testWidgets('leaving after the start is refused with a reason', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/trips/occ1': tripJson,
      'GET /v1/trips/occ1/live': liveJson,
      'DELETE /v1/bookings/b1': const Fail(409, 'departed'),
    });
    await tester.pumpWidget(app(tripScreen(f.api)));
    await tester.pumpAndSettle();
    await scrollTo(tester, find.text('Δεν θα έρθω'));
    await tester.tap(find.text('Δεν θα έρθω'));
    await tester.pumpAndSettle();
    await tester.tap(inDialog('Δεν θα έρθω'));
    await tester.pumpAndSettle();
    expect(find.text('Η διαδρομή έχει ήδη ξεκινήσει'), findsOneWidget);
  });

  testWidgets('the driver calls off a departure and the passengers are counted', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/trips/occ1': {...tripJson, 'role': 'driver'},
      'GET /v1/trips/occ1/live': liveJson,
      'DELETE /v1/trips/occ1': const NoContent(),
    });
    await tester.pumpWidget(app(tripScreen(f.api)));
    await tester.pumpAndSettle();
    await scrollTo(tester, find.text('Ακύρωση διαδρομής'));
    await tester.tap(find.text('Ακύρωση διαδρομής'));
    await tester.pumpAndSettle();
    expect(find.text('Η διαδρομή δεν θα γίνει. Θα ειδοποιηθεί 1 επιβάτης.'), findsOneWidget);
    await tester.tap(inDialog('Ακύρωση διαδρομής'));
    await tester.pumpAndSettle();
    expect(f.calls, contains('DELETE /v1/trips/occ1'));
    expect(find.text('Η διαδρομή ακυρώθηκε'), findsOneWidget);
  });

  testWidgets('no cancel buttons once the car has left', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/trips/occ1': {...tripJson, 'role': 'driver', 'drive_set_at': '2026-07-30T04:30:00Z'},
      'GET /v1/trips/occ1/live': liveJson,
    });
    await tester.pumpWidget(app(TripScreen(
      api: f.api,
      tripId: 'occ1',
      mapBuilder: fakeMap,
      locate: () async => null,
      livePoll: const Duration(minutes: 5),
      chatPoll: const Duration(minutes: 5),
      onStartDrive: (_) async {},
    )));
    await tester.pumpAndSettle();
    expect(find.text('Ακύρωση διαδρομής', skipOffstage: false), findsNothing);
  });

  testWidgets('a completed trip has no «Δεν θα έρθω»', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/trips/occ1': {...tripJson, 'status': 'completed'},
      'GET /v1/trips/occ1/live': liveJson,
    });
    await tester.pumpWidget(app(tripScreen(f.api)));
    await tester.pumpAndSettle();
    expect(find.text('Δεν θα έρθω', skipOffstage: false), findsNothing);
  });

  // ---------------------------------------------------------------- my routes

  testWidgets('my routes: a card per route, the nudge switch, retiring', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/me/routes': {'routes': [routeJson]},
      'PUT /v1/me/routes/r1/nudge': <String, Object>{},
      'DELETE /v1/me/routes/r1': const NoContent(),
    });
    await tester.pumpWidget(app(MyRoutesScreen(api: f.api)));
    await tester.pumpAndSettle();

    expect(find.text('Οι διαδρομές μου'), findsOneWidget);
    expect(find.textContaining('07:30'), findsWidgets);
    expect(find.textContaining('Αλεξανδρούπολη → Φέρες'), findsOneWidget);
    expect(find.textContaining('17 χλμ'), findsOneWidget);
    expect(find.text('2 προγραμματισμένα δρομολόγια'), findsOneWidget);
    expect(find.text('Ο σύνδεσμος άνοιξε 5 φορές'), findsOneWidget);

    await tester.tap(find.byType(Switch));
    await tester.pumpAndSettle();
    expect(f.bodies['PUT /v1/me/routes/r1/nudge'], {'on': true});

    await tester.tap(find.text('Απόσυρση'));
    await tester.pumpAndSettle();
    expect(
      find.text('Η διαδρομή δεν θα προσφέρεται πια και τα άδεια δρομολόγιά της ακυρώνονται. '
          'Μένουν 1 με επιβάτες: ακύρωσέ τα από το καθένα αν χρειάζεται, για να ειδοποιηθούν.'),
      findsOneWidget,
    );
    await tester.tap(inDialog('Απόσυρση'));
    await tester.pumpAndSettle();
    expect(f.calls, contains('DELETE /v1/me/routes/r1'));
    expect(f.calls.where((c) => c == 'GET /v1/me/routes').length, 2, reason: 'the list is read again');
  });

  testWidgets('one upcoming trip and one link open read in the singular', (tester) async {
    await bigPhone(tester);
    final f = fakeApi({
      'GET /v1/me/routes': {
        'routes': [
          {...routeJson, 'upcoming_trips': 1, 'link_opens': 1, 'kept_trips': 0},
        ],
      },
    });
    await tester.pumpWidget(app(MyRoutesScreen(api: f.api)));
    await tester.pumpAndSettle();
    expect(find.text('1 προγραμματισμένο δρομολόγιο'), findsOneWidget);
    expect(find.text('Ο σύνδεσμος άνοιξε 1 φορά'), findsOneWidget);
  });
}

void chatPagingTests() {
  testWidgets('chat reads past the first 50 messages and polls only for newer ones', (tester) async {
    await bigPhone(tester);
    final queries = <String>[];
    Map<String, Object?> msg(int i) => {
          'id': 'm$i',
          'sender_id': 'd1',
          'sender_name': 'Νίκος',
          'body': 'μήνυμα $i',
          'created_at': DateTime.utc(2026, 7, 30, 4, 0, i).toIso8601String(),
        };
    var total = 55;
    final api = Api(
      baseUrl: 'http://api',
      tokens: MemoryTokenStore(access: 'a', refresh: 'r'),
      client: MockClient((req) async {
        queries.add(req.url.query);
        final after = req.url.queryParameters['after_id'];
        final start = after == null ? 0 : int.parse(after.substring(1)) + 1;
        final page = [for (var i = start; i < total && i < start + 50; i++) msg(i)];
        return http.Response(jsonEncode({'messages': page}), 200,
            headers: {'content-type': 'application/json; charset=utf-8'});
      }),
    );
    await tester.pumpWidget(app(Scaffold(
      body: SingleChildScrollView(
        child: TripChat(api: api, tripId: 'occ1', myId: 'p1', poll: const Duration(seconds: 10)),
      ),
    )));
    await tester.pumpAndSettle();
    expect(find.text('μήνυμα 54', skipOffstage: false), findsOneWidget);
    expect(queries, [
      '',
      'after_time=${Uri.encodeQueryComponent(DateTime.utc(2026, 7, 30, 4, 0, 49).toIso8601String())}&after_id=m49',
    ]);

    total = 56;
    await tester.pump(const Duration(seconds: 10));
    await tester.pumpAndSettle();
    expect(queries.last, contains('after_id=m54'));
    expect(find.text('μήνυμα 55', skipOffstage: false), findsOneWidget);
    expect(find.text('μήνυμα 0', skipOffstage: false), findsOneWidget, reason: 'old messages stay');
  });
  testWidgets('a message sent here does not hide one somebody sent just before it', (tester) async {
    await bigPhone(tester);
    final server = <Map<String, Object?>>[messageJson('m1', 'd1', 'Νίκος', 'πρώτο')];
    final afters = <String?>[];
    final api = Api(
      baseUrl: 'http://api',
      tokens: MemoryTokenStore(access: 'a', refresh: 'r'),
      client: MockClient((req) async {
        if (req.method == 'POST') {
          final mine = {
            ...messageJson('m3', 'p1', 'Ελένη', 'δικό μου'),
            'created_at': '2026-07-30T04:02:00Z',
          };
          // Somebody else's message reached the server a moment earlier.
          server.add({...messageJson('m2', 'd1', 'Νίκος', 'δεύτερο'), 'created_at': '2026-07-30T04:01:00Z'});
          server.add(mine);
          return http.Response(jsonEncode(mine), 201, headers: {'content-type': 'application/json; charset=utf-8'});
        }
        final afterId = req.url.queryParameters['after_id'];
        afters.add(afterId);
        final start = afterId == null ? 0 : server.indexWhere((m) => m['id'] == afterId) + 1;
        return http.Response(jsonEncode({'messages': server.sublist(start)}), 200,
            headers: {'content-type': 'application/json; charset=utf-8'});
      }),
    );
    await tester.pumpWidget(app(Scaffold(
      body: SingleChildScrollView(
        child: TripChat(api: api, tripId: 'occ1', myId: 'p1', poll: const Duration(seconds: 10)),
      ),
    )));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'δικό μου');
    await tester.pump();
    await tester.tap(find.widgetWithText(FilledButton, 'Στείλε'));
    await tester.pumpAndSettle();
    await tester.pump(const Duration(seconds: 10));
    await tester.pumpAndSettle();

    expect(afters.last, 'm1', reason: 'the cursor is the last message read, not the one sent');
    expect(find.text('δεύτερο'), findsOneWidget);
    double y(String t) => tester.getTopLeft(find.text(t)).dy;
    expect(y('πρώτο') < y('δεύτερο') && y('δεύτερο') < y('δικό μου'), isTrue, reason: 'in the order they were sent');
  });
}

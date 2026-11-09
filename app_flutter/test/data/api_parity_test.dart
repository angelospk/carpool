// The calls behind account deletion, blocking, reports, chat, ratings,
// cancelling and the driver's own routes: paths, bodies and parsed answers.

import 'package:carpool/data/api.dart';
import 'package:carpool/data/models.dart';
import 'package:flutter_test/flutter_test.dart';

import '../ui/harness.dart';
import 'fixtures.dart';

void main() {
  test('deleting the account asks for a code, then sends it', () async {
    final f = fakeApi({
      'POST /v1/me/delete-code': {'dev_code': '123456'},
      'DELETE /v1/me': const NoContent(),
    });
    expect(await f.api.requestDeleteCode(), '123456');
    await f.api.deleteAccount('123456');
    expect(f.calls, ['POST /v1/me/delete-code', 'DELETE /v1/me']);
    expect(f.bodies['DELETE /v1/me'], {'code': '123456'});
  });

  test('outside dev mode the delete code comes by SMS only', () async {
    final f = fakeApi({'POST /v1/me/delete-code': const NoContent()});
    expect(await f.api.requestDeleteCode(), isNull);
  });

  test('a wrong delete code is an ApiException with its code', () async {
    final f = fakeApi({'DELETE /v1/me': const Fail(403, 'bad_code')});
    await expectLater(
      f.api.deleteAccount('000000'),
      throwsA(isA<ApiException>().having((e) => e.code, 'code', 'bad_code')),
    );
  });

  test('block, unblock, the blocked list and a report', () async {
    final f = fakeApi({
      'POST /v1/users/u2/block': const NoContent(),
      'DELETE /v1/users/u2/block': const NoContent(),
      'GET /v1/me/blocks': {
        'blocked': [
          {'id': 'u2', 'name': 'Κώστας', 'photo_url': null},
        ],
      },
      'POST /v1/users/u2/report': const NoContent(),
    });
    await f.api.block('u2');
    await f.api.unblock('u2');
    final blocked = await f.api.blocks();
    expect(blocked.single.id, 'u2');
    expect(blocked.single.name, 'Κώστας');
    await f.api.report('u2', 'unsafe_driving');
    expect(f.bodies['POST /v1/users/u2/report'], {'reason': 'unsafe_driving'});
    expect(f.calls, [
      'POST /v1/users/u2/block',
      'DELETE /v1/users/u2/block',
      'GET /v1/me/blocks',
      'POST /v1/users/u2/report',
    ]);
  });

  test('chat: read the messages, send one', () async {
    final f = fakeApi({
      'GET /v1/trips/occ1/messages': {
        'messages': [messageJson('m1', 'd1', 'Νίκος', 'Καλημέρα')],
      },
      'POST /v1/trips/occ1/messages': messageJson('m2', 'p1', 'Ελένη', 'Έρχομαι'),
    });
    final messages = await f.api.messages('occ1');
    expect(messages.single.id, 'm1');
    expect(messages.single.senderId, 'd1');
    expect(messages.single.senderName, 'Νίκος');
    expect(messages.single.body, 'Καλημέρα');
    expect(messages.single.createdAt, DateTime.utc(2026, 7, 30, 4, 0));
    final sent = await f.api.sendMessage('occ1', 'Έρχομαι');
    expect(sent.id, 'm2');
    expect(f.bodies['POST /v1/trips/occ1/messages'], {'body': 'Έρχομαι'});
  });

  test('a rating names who it is for', () async {
    final f = fakeApi({'POST /v1/trips/occ1/rate': <String, Object>{}});
    await f.api.rate('occ1', rateeId: 'd1', stars: 4);
    expect(f.bodies['POST /v1/trips/occ1/rate'], {'stars': 4, 'ratee_id': 'd1'});
  });

  test('a passenger leaves a booking, a driver calls off a departure', () async {
    final f = fakeApi({
      'DELETE /v1/bookings/b1': const NoContent(),
      'DELETE /v1/trips/occ1': const NoContent(),
    });
    await f.api.cancelBooking('b1');
    await f.api.cancelTrip('occ1');
    expect(f.calls, ['DELETE /v1/bookings/b1', 'DELETE /v1/trips/occ1']);
  });

  test('the driver\'s routes: list, nudge, retire', () async {
    final f = fakeApi({
      'GET /v1/me/routes': {'routes': [routeJson]},
      'PUT /v1/me/routes/r1/nudge': <String, Object>{},
      'DELETE /v1/me/routes/r1': const NoContent(),
    });
    final routes = await f.api.myRoutes();
    final route = routes.single;
    expect(route.id, 'r1');
    expect(route.originLabel, 'Αλεξανδρούπολη');
    expect(route.destinationLabel, 'Φέρες');
    expect(route.routeKm, 17.0);
    expect(route.seats, 3);
    expect(route.kind, 'weekly');
    expect(route.weekdays, [1, 2, 3, 4, 5]);
    expect(route.oneoffDate, isNull);
    expect(route.departEarliest, '07:30');
    expect(route.departLatest, '08:00');
    expect(route.upcomingTrips, 2);
    expect(route.keptTrips, 1);
    expect(route.linkOpens, 5);
    expect(route.nudgeWhenLeaving, isFalse);
    await f.api.setNudge('r1', true);
    expect(f.bodies['PUT /v1/me/routes/r1/nudge'], {'on': true});
    await f.api.retireRoute('r1');
    expect(f.calls.last, 'DELETE /v1/me/routes/r1');
  });

  test('a trip tells who can be rated and who the caller is', () {
    final trip = TripDetail.fromJson({
      ...tripJson,
      'driver': {...(tripJson['driver'] as Map), 'can_rate': true, 'rated': false},
    });
    expect(trip.driver.canRate, isTrue);
    expect(trip.driver.rated, isFalse);
    expect(trip.passengers.single.canRate, isFalse);
    // The passenger's own pick-up says which passenger the caller is.
    expect(trip.myId, 'p1');
    final driving = TripDetail.fromJson({...tripJson, 'role': 'driver'});
    expect(driving.myId, 'd1');
  });
}

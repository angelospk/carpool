import 'dart:async';
import 'dart:convert';

import 'package:carpool/core/geo.dart';
import 'package:carpool/data/api.dart';
import 'package:carpool/data/models.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'fixtures.dart';

http.Response json(Object body, [int status = 200]) => http.Response(jsonEncode(body), status,
    headers: {'content-type': 'application/json; charset=utf-8'});

void main() {
  test('an expired access token is refreshed once and the call retried', () async {
    final seen = <String>[];
    final tokens = MemoryTokenStore(access: 'old', refresh: 'r1');
    final api = Api(
      baseUrl: 'http://api',
      tokens: tokens,
      client: MockClient((req) async {
        seen.add('${req.method} ${req.url.path} ${req.headers['authorization'] ?? ''}');
        if (req.url.path == '/v1/auth/refresh') {
          expect(jsonDecode(req.body)['refresh_token'], 'r1');
          return json({'access_token': 'new', 'refresh_token': 'r2'});
        }
        if (req.headers['authorization'] == 'Bearer old') return json({}, 401);
        return json({'id': 'u1', 'phone': '+30690', 'name': 'Ελένη'});
      }),
    );
    final me = await api.me();
    expect(me.name, 'Ελένη');
    expect(await tokens.read(), (access: 'new', refresh: 'r2'));
    expect(seen, ['GET /v1/me Bearer old', 'POST /v1/auth/refresh ', 'GET /v1/me Bearer new']);
  });

  test('a refused refresh ends the session', () async {
    var ended = false;
    final tokens = MemoryTokenStore(access: 'old', refresh: 'r1');
    final api = Api(
      baseUrl: 'http://api',
      tokens: tokens,
      onSessionEnded: () => ended = true,
      client: MockClient((req) async => json({}, 401)),
    );
    await expectLater(api.me(), throwsA(isA<ApiException>().having((e) => e.status, 'status', 401)));
    expect(ended, isTrue);
    expect(await tokens.read(), isNull);
  });

  test('a refresh that lands after sign-out does not sign the person back in', () async {
    final tokens = MemoryTokenStore(access: 'old', refresh: 'r1');
    final api = Api(
      baseUrl: 'http://api',
      tokens: tokens,
      client: MockClient((req) async {
        if (req.url.path == '/v1/auth/refresh') {
          // Signed out while the refresh was on the wire.
          await tokens.clear();
          return json({'access_token': 'new', 'refresh_token': 'r2'});
        }
        if (req.headers['authorization'] == 'Bearer new') {
          return json({'id': 'u1', 'phone': '+30690', 'name': 'Ελένη'});
        }
        return json({}, 401);
      }),
    );
    await expectLater(api.me(), throwsA(isA<ApiException>()));
    expect(await tokens.read(), isNull);
  });

  test('signing out while a refresh is on the wire leaves nobody signed in', () async {
    final answer = Completer<http.Response>();
    final tokens = MemoryTokenStore(access: 'old', refresh: 'r1');
    final api = Api(
      baseUrl: 'http://api',
      tokens: tokens,
      client: MockClient((req) async {
        if (req.url.path == '/v1/auth/refresh') return answer.future;
        if (req.headers['authorization'] == 'Bearer new') {
          return json({'id': 'u1', 'phone': '+30690', 'name': 'Ελένη'});
        }
        return json({}, 401);
      }),
    );
    final call = api.me().then((_) {}, onError: (_) {});
    await Future<void>.delayed(Duration.zero);
    final out = api.signOut();
    answer.complete(json({'access_token': 'new', 'refresh_token': 'r2'}));
    await out;
    await call;
    expect(await tokens.read(), isNull);
  });

  test('a refresh whose answer was lost is retried with the same attempt id', () async {
    final attempts = <String>[];
    var fail = true;
    final tokens = MemoryTokenStore(access: 'old', refresh: 'r1');
    final api = Api(
      baseUrl: 'http://api',
      tokens: tokens,
      client: MockClient((req) async {
        if (req.url.path == '/v1/auth/refresh') {
          attempts.add(jsonDecode(req.body)['attempt_id'] as String);
          if (fail) {
            fail = false;
            throw http.ClientException('connection reset');
          }
          return json({'access_token': 'new', 'refresh_token': 'r2'});
        }
        if (req.headers['authorization'] == 'Bearer old') return json({}, 401);
        return json({'id': 'u1', 'phone': '+30690', 'name': 'Ελένη'});
      }),
    );
    await expectLater(api.me(), throwsA(isA<ApiException>()));
    expect((await api.me()).name, 'Ελένη');
    expect(attempts, hasLength(2));
    expect(attempts[1], attempts[0], reason: 'a new id for the same token reads as reuse and ends the family');
  });

  test('the server error code reaches the caller', () async {
    final api = Api(
      baseUrl: 'http://api',
      tokens: MemoryTokenStore(access: 'a', refresh: 'r'),
      client: MockClient((req) async =>
          json({'error': {'code': 'duplicate_route', 'message': 'you already offer this route'}}, 409)),
    );
    await expectLater(
        api.upcomingTrips(), throwsA(isA<ApiException>().having((e) => e.code, 'code', 'duplicate_route')));
  });

  test('a trip reads with its stops, car and pickup', () async {
    final api = Api(
      baseUrl: 'http://api',
      tokens: MemoryTokenStore(access: 'a', refresh: 'r'),
      client: MockClient((req) async {
        expect(req.url.path, '/v1/trips/occ1');
        return json(tripJson);
      }),
    );
    final t = await api.trip('occ1');
    expect(t.role, TripRole.passenger);
    expect(t.driver.carPlate, 'ΕΒΡ 4521');
    expect(t.route.length, 3);
    final pickup = t.myPickup!;
    expect(pickup.label, 'Πλ. Δημαρχείου');
    expect(pickup.point, const LatLon(40.85, 25.85));
    expect(t.pickupAt, DateTime.utc(2026, 7, 30, 4, 38));
    expect(t.departEarliest, DateTime.utc(2026, 7, 30, 4, 30));
  });

  test('live reads the driver and the ETA', () async {
    final api = Api(
      baseUrl: 'http://api',
      tokens: MemoryTokenStore(access: 'a', refresh: 'r'),
      client: MockClient((req) async => json(liveJson)),
    );
    final l = await api.live('occ1');
    expect(l.positions.single.name, 'Νίκος');
    expect(l.positions.single.at, DateTime.utc(2026, 7, 30, 4, 14, 50));
    expect(l.driverEtaMin, 8);
  });

  test('a preview gives each road, with a via on the alternatives', () async {
    final api = Api(
      baseUrl: 'http://api',
      tokens: MemoryTokenStore(access: 'a', refresh: 'r'),
      client: MockClient((req) async {
        expect(req.method, 'POST');
        expect(req.url.path, '/v1/routes/preview');
        expect(jsonDecode(req.body),
            {'origin_lon': 25.8, 'origin_lat': 40.85, 'destination_lon': 26.0, 'destination_lat': 40.85});
        return json(previewJson);
      }),
    );
    final roads = await api.routePreview(const LatLon(40.85, 25.8), const LatLon(40.85, 26.0));
    expect(roads.length, 2);
    expect(roads[0].via, isNull);
    expect(roads[1].via, const LatLon(40.95, 25.9));
    expect(roads[1].minutes, 25);
    expect(roads[1].km, 26.0);
    expect(roads[1].line.length, 3);
  });

  test('publishing sends the via as [lon, lat]', () async {
    Map<String, dynamic>? sent;
    final api = Api(
      baseUrl: 'http://api',
      tokens: MemoryTokenStore(access: 'a', refresh: 'r'),
      client: MockClient((req) async {
        sent = jsonDecode(req.body) as Map<String, dynamic>;
        return json({'template_id': 't9', 'route_km': 26.0, 'ends_at_port': false, 'first_occurrence_id': 'o9'}, 201);
      }),
    );
    final r = await api.publishRoute(RouteDraft(
      origin: const Place(id: 'p1', name: 'Α', area: '', lat: 40.85, lon: 25.8),
      destination: const Place(id: 'p2', name: 'Β', area: '', lat: 40.85, lon: 26.0),
      via: const LatLon(40.95, 25.9),
      seats: 3,
      oneoffDate: '2026-07-30',
      departEarliest: '07:30',
      departLatest: '07:45',
    ));
    expect(r.templateId, 't9');
    expect(sent!['via'], [25.9, 40.95]);
    expect(sent!['kind'], 'oneoff');
    expect(sent!['origin_label'], 'Α');
    expect(sent!['seats'], 3);
  });

  test('sharing a trip gives the link to send', () async {
    final api = Api(
      baseUrl: 'http://api',
      tokens: MemoryTokenStore(access: 'a', refresh: 'r'),
      client: MockClient((req) async {
        expect('${req.method} ${req.url.path}', 'POST /v1/trips/occ1/share');
        return json({'token': 'tok', 'url': 'https://carpool.haroldpoi.dev/t/tok'});
      }),
    );
    expect((await api.shareTrip('occ1')).url, 'https://carpool.haroldpoi.dev/t/tok');
  });

  test('an ask reads its detour', () async {
    final api = Api(
      baseUrl: 'http://api',
      tokens: MemoryTokenStore(access: 'a', refresh: 'r'),
      client: MockClient((req) async => json(askJson)),
    );
    final a = await api.ask('a1');
    expect(a.detourMin, 6.4);
    expect(a.detourKm, 2.13);
    expect(a.passengerName, 'Μαρία');
  });
}

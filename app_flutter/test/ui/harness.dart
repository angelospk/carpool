import 'dart:convert';

import 'package:carpool/data/api.dart';
import 'package:carpool/ui/map/map_spec.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// A map that draws nothing and says what it was asked to draw.
Widget fakeMap(BuildContext context, MapSpec spec) => Text(
      'pins:${spec.pins.map((p) => p.kind.name).join(',')} '
      'lines:${spec.lines.map((l) => l.style.name).join(',')}',
      key: const Key('map'),
    );

/// A server answer that is an error with this status and code.
class Fail {
  const Fail(this.status, this.code);
  final int status;
  final String code;
}

/// A server answer with no body (204).
class NoContent {
  const NoContent();
}

/// An Api whose server is a table of "METHOD /path" → JSON, recording calls.
({Api api, List<String> calls, Map<String, Object?> bodies}) fakeApi(Map<String, Object> routes) {
  final calls = <String>[];
  final bodies = <String, Object?>{};
  final api = Api(
    baseUrl: 'http://api',
    tokens: MemoryTokenStore(access: 'a', refresh: 'r'),
    client: MockClient((req) async {
      final key = '${req.method} ${req.url.path}';
      calls.add(key);
      bodies[key] = req.body.isEmpty ? null : jsonDecode(req.body);
      final body = routes[key];
      if (body == null) return http.Response('{"error":{"code":"not_found","message":"x"}}', 404);
      if (body is Fail) {
        return http.Response('{"error":{"code":"${body.code}","message":"x"}}', body.status,
            headers: {'content-type': 'application/json; charset=utf-8'});
      }
      if (body is NoContent) return http.Response('', 204);
      return http.Response(jsonEncode(body), key.startsWith('POST /v1/routes') && key == 'POST /v1/routes' ? 201 : 200,
          headers: {'content-type': 'application/json; charset=utf-8'});
    }),
  );
  return (api: api, calls: calls, bodies: bodies);
}

Future<void> bigPhone(WidgetTester tester) async {
  tester.view.physicalSize = const Size(1080, 2340);
  tester.view.devicePixelRatio = 2.6;
  addTearDown(tester.view.reset);
}

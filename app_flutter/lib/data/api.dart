import 'dart:convert';
import 'dart:math' as math;

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;

import '../core/geo.dart';
import '../core/polyline.dart';
import 'models.dart';

/// The session tokens, as the app keeps them.
abstract class TokenStore {
  Future<({String access, String refresh})?> read();

  Future<void> write(String access, String refresh);

  Future<void> clear();
}

/// Tokens held in memory: for tests and for a session that must not survive.
class MemoryTokenStore implements TokenStore {
  MemoryTokenStore({String? access, String? refresh})
      : _pair = (access == null || refresh == null)
            ? null
            : (access: access, refresh: refresh);

  ({String access, String refresh})? _pair;

  @override
  Future<({String access, String refresh})?> read() async => _pair;

  @override
  Future<void> write(String access, String refresh) async {
    _pair = (access: access, refresh: refresh);
  }

  @override
  Future<void> clear() async {
    _pair = null;
  }
}

/// Tokens kept in the platform's secure storage.
class SecureTokenStore implements TokenStore {
  SecureTokenStore({FlutterSecureStorage? storage})
      : _storage = storage ?? const FlutterSecureStorage();

  static const String _accessKey = 'carpool_access_token';
  static const String _refreshKey = 'carpool_refresh_token';

  final FlutterSecureStorage _storage;

  @override
  Future<({String access, String refresh})?> read() async {
    final access = await _storage.read(key: _accessKey);
    final refresh = await _storage.read(key: _refreshKey);
    if (access == null || refresh == null) return null;
    return (access: access, refresh: refresh);
  }

  @override
  Future<void> write(String access, String refresh) async {
    await _storage.write(key: _accessKey, value: access);
    await _storage.write(key: _refreshKey, value: refresh);
  }

  @override
  Future<void> clear() async {
    await _storage.delete(key: _accessKey);
    await _storage.delete(key: _refreshKey);
  }
}

/// What the server refused with: its envelope, as `{"error":{"code","message"}}`.
class ApiException implements Exception {
  const ApiException(this.status, this.code, this.message);

  final int status;
  final String code;
  final String message;

  @override
  String toString() => 'ApiException($status, $code): $message';
}

/// The JSON API, with the one refresh shared by every call that needs it.
/// How many messages one chat read returns at most.
const messagePage = 50;

class Api {
  Api({
    required String baseUrl,
    required this.tokens,
    http.Client? client,
    this.onSessionEnded,
  })  : _base = baseUrl.replaceAll(RegExp(r'/+$'), ''),
        _client = client ?? http.Client();

  final String _base;
  final http.Client _client;
  final TokenStore tokens;
  final void Function()? onSessionEnded;

  /// The single refresh in flight, shared by every request that gets a 401.
  Future<bool>? _refreshing;

  /// The attempt id sent with a refresh token whose answer never arrived. The
  /// server rotates on the first use; a retry under a new id reads as a stolen
  /// token being replayed and ends every session of the family.
  String? _attemptToken;
  String? _attemptId;

  // ---------------------------------------------------------------- plumbing

  Future<http.Response> _send(
    String method,
    String path,
    Object? body,
    bool anonymous,
  ) async {
    final access = anonymous ? null : (await tokens.read())?.access;
    final request = http.Request(method, Uri.parse('$_base$path'));
    if (access != null) request.headers['authorization'] = 'Bearer $access';
    if (body != null) {
      request.headers['content-type'] = 'application/json; charset=utf-8';
      request.body = jsonEncode(body);
    }
    final streamed = await _client.send(request);
    return http.Response.fromStream(streamed);
  }

  Object? _decode(http.Response response) {
    if (response.body.isEmpty) return null;
    try {
      return jsonDecode(utf8.decode(response.bodyBytes));
    } catch (_) {
      return null;
    }
  }

  ApiException _exception(http.Response response) {
    final body = _decode(response);
    if (body is Map) {
      final error = body['error'];
      if (error is Map) {
        return ApiException(
          response.statusCode,
          error['code'] as String? ?? 'unknown',
          error['message'] as String? ?? 'the request failed',
        );
      }
    }
    return ApiException(response.statusCode, 'unknown', 'the request failed');
  }

  /// Sends a request, refreshing the session once if the server says 401.
  Future<Object?> _call(
    String method,
    String path, {
    Object? body,
    bool anonymous = false,
  }) async {
    var response = await _send(method, path, body, anonymous);
    if (response.statusCode == 401 && !anonymous) {
      final refreshed = await _refreshOnce();
      if (!refreshed) {
        throw const ApiException(401, 'unauthorized', 'the session is over');
      }
      response = await _send(method, path, body, anonymous);
      if (response.statusCode == 401) {
        await tokens.clear();
        onSessionEnded?.call();
        throw const ApiException(401, 'unauthorized', 'the session is over');
      }
    }
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw _exception(response);
    }
    return _decode(response);
  }

  /// A map body, or an empty map when the server sent nothing.
  Map<String, dynamic> _map(Object? body) {
    if (body is Map<String, dynamic>) return body;
    if (body is Map) return body.cast<String, dynamic>();
    return const {};
  }

  /// A list of maps under [key], or empty when the server sent nothing.
  List<Map<String, dynamic>> _list(Object? body, String key) {
    final value = _map(body)[key];
    if (value is! List) return const [];
    return [
      for (final item in value)
        if (item is Map) item.cast<String, dynamic>(),
    ];
  }

  /// Waits out a refresh on the wire, so that its answer cannot land on a
  /// session that changed meanwhile.
  Future<void> _settled() async {
    try {
      await _refreshing;
    } catch (_) {
      // A failed refresh changed nothing worth waiting for.
    }
  }

  /// Forgets the session on this phone.
  Future<void> signOut() async {
    await _settled();
    await tokens.clear();
  }

  Future<bool> _refreshOnce() {
    final running = _refreshing;
    if (running != null) return running;
    final fresh = _refreshTokens();
    _refreshing = fresh;
    fresh.whenComplete(() {
      if (_refreshing == fresh) _refreshing = null;
    });
    return fresh;
  }

  /// Rotates the tokens. Only an explicit refusal ends the session.
  Future<bool> _refreshTokens() async {
    final stored = await tokens.read();
    if (stored == null || stored.refresh.isEmpty) return false;
    if (_attemptToken != stored.refresh) {
      _attemptToken = stored.refresh;
      _attemptId = _newAttemptId();
    }
    final attemptId = _attemptId!;
    http.Response response;
    try {
      response = await _client.post(
        Uri.parse('$_base/v1/auth/refresh'),
        headers: const {'content-type': 'application/json; charset=utf-8'},
        body: jsonEncode({'refresh_token': stored.refresh, 'attempt_id': attemptId}),
      );
    } catch (_) {
      return false;
    }
    // Signed out, or into another account, while this was on the wire: the
    // answer is about a session that no longer exists here.
    if ((await tokens.read())?.refresh != stored.refresh) return false;
    if (response.statusCode == 401 || response.statusCode == 403) {
      await tokens.clear();
      onSessionEnded?.call();
      return false;
    }
    if (response.statusCode < 200 || response.statusCode >= 300) return false;
    final body = _map(_decode(response));
    final access = body['access_token'];
    final refresh = body['refresh_token'];
    if (access is! String || refresh is! String) return false;
    _attemptToken = null;
    _attemptId = null;
    await tokens.write(access, refresh);
    return true;
  }

  static final math.Random _random = math.Random.secure();

  String _newAttemptId() {
    final buffer = StringBuffer();
    for (var i = 0; i < 16; i++) {
      buffer.write(_random.nextInt(256).toRadixString(16).padLeft(2, '0'));
    }
    return buffer.toString();
  }

  // ------------------------------------------------------------------- auth

  Future<String?> requestOtp(String phone) async {
    final body = _map(await _call(
      'POST',
      '/v1/auth/otp/request',
      body: {'phone': phone},
      anonymous: true,
    ));
    return body['dev_code'] as String?;
  }

  Future<VerifyResult> verifyOtp(String phone, String code) async {
    final body = _map(await _call(
      'POST',
      '/v1/auth/otp/verify',
      body: {'phone': phone, 'code': code},
      anonymous: true,
    ));
    final result = VerifyResult.fromJson(body);
    if (result.accessToken != null && result.refreshToken != null) {
      await _settled();
      await tokens.write(result.accessToken!, result.refreshToken!);
    }
    return result;
  }

  Future<void> completeSignup({
    required String signupToken,
    required String name,
    required String homePlaceId,
    required String dateOfBirth,
    String? inviteCode,
  }) async {
    final body = _map(await _call(
      'POST',
      '/v1/auth/signup/complete',
      body: {
        'signup_token': signupToken,
        'name': name,
        'home_place_id': homePlaceId,
        'date_of_birth': dateOfBirth,
        'invite_code': ?inviteCode,
      },
      anonymous: true,
    ));
    final access = body['access_token'];
    final refresh = body['refresh_token'];
    if (access is String && refresh is String) {
      await _settled();
      await tokens.write(access, refresh);
    }
  }

  Future<Me> me() async => Me.fromJson(_map(await _call('GET', '/v1/me')));

  /// The SMS code that deletes the account, when the server is in dev mode.
  Future<String?> requestDeleteCode() async {
    final body = _map(await _call('POST', '/v1/me/delete-code'));
    return body['dev_code'] as String?;
  }

  Future<void> deleteAccount(String code) async {
    await _call('DELETE', '/v1/me', body: {'code': code});
  }

  // --------------------------------------------------------------- blocking

  Future<void> block(String id) async {
    await _call('POST', '/v1/users/$id/block');
  }

  Future<void> unblock(String id) async {
    await _call('DELETE', '/v1/users/$id/block');
  }

  Future<List<BlockedUser>> blocks() async {
    final body = await _call('GET', '/v1/me/blocks');
    return [for (final u in _list(body, 'blocked')) BlockedUser.fromJson(u)];
  }

  Future<void> report(String id, String reason) async {
    await _call('POST', '/v1/users/$id/report', body: {'reason': reason});
  }

  // ------------------------------------------------------------------- chat

  /// The server sends at most [messagePage] messages, oldest first; [after]
  /// asks for the ones that came after it.
  Future<List<ChatMessage>> messages(String tripId, {ChatMessage? after}) async {
    final cursor = after == null
        ? ''
        : '?after_time=${Uri.encodeQueryComponent(after.createdAt.toIso8601String())}'
            '&after_id=${Uri.encodeQueryComponent(after.id)}';
    final body = await _call('GET', '/v1/trips/$tripId/messages$cursor');
    return [for (final m in _list(body, 'messages')) ChatMessage.fromJson(m)];
  }

  Future<ChatMessage> sendMessage(String tripId, String body) async =>
      ChatMessage.fromJson(_map(await _call(
        'POST',
        '/v1/trips/$tripId/messages',
        body: {'body': body},
      )));

  // --------------------------------------------------------- rate, cancel

  Future<void> rate(String tripId, {required String rateeId, required int stars}) async {
    await _call('POST', '/v1/trips/$tripId/rate', body: {
      'stars': stars,
      'ratee_id': rateeId,
    });
  }

  Future<void> cancelBooking(String bookingId) async {
    await _call('DELETE', '/v1/bookings/$bookingId');
  }

  Future<void> cancelTrip(String tripId) async {
    await _call('DELETE', '/v1/trips/$tripId');
  }

  // -------------------------------------------------------- the driver's routes

  Future<List<MyRoute>> myRoutes() async {
    final body = await _call('GET', '/v1/me/routes');
    return [for (final r in _list(body, 'routes')) MyRoute.fromJson(r)];
  }

  Future<void> setNudge(String id, bool on) async {
    await _call('PUT', '/v1/me/routes/$id/nudge', body: {'on': on});
  }

  Future<void> retireRoute(String id) async {
    await _call('DELETE', '/v1/me/routes/$id');
  }

  // ------------------------------------------------------------------ trips

  Future<List<TripSummary>> upcomingTrips() async {
    final body = await _call('GET', '/v1/trips/upcoming');
    return [for (final t in _list(body, 'trips')) TripSummary.fromJson(t)];
  }

  Future<TripDetail> trip(String id) async =>
      TripDetail.fromJson(_map(await _call('GET', '/v1/trips/$id')));

  Future<LiveState> live(String id) async =>
      LiveState.fromJson(_map(await _call('GET', '/v1/trips/$id/live')));

  Future<void> putPosition(
    String id,
    double lat,
    double lon, {
    double? accuracyM,
    DateTime? observedAt,
  }) async {
    await _call('PUT', '/v1/trips/$id/position', body: {
      'lat': lat,
      'lon': lon,
      'accuracy_m': ?accuracyM,
      if (observedAt != null) 'observed_at': observedAt.toUtc().toIso8601String(),
    });
  }

  Future<void> stopPosition(String id) async {
    await _call('DELETE', '/v1/trips/$id/position');
  }

  Future<void> startDrive(String id, DateTime departsAt) async {
    await _call('POST', '/v1/trips/$id/drive', body: {
      'departs_at': departsAt.toUtc().toIso8601String(),
    });
  }

  Future<void> endDrive(String id) async {
    await _call('POST', '/v1/trips/$id/drive/end');
  }

  Future<void> confirmTrip(String id) async {
    await _call('POST', '/v1/trips/$id/confirm');
  }

  Future<SharedLink> shareTrip(String id) async =>
      SharedLink.fromJson(_map(await _call('POST', '/v1/trips/$id/share')));

  Future<void> revokeTripShare(String id) async {
    await _call('DELETE', '/v1/trips/$id/share');
  }

  // ------------------------------------------------------------------- asks

  Future<List<PendingAsk>> pendingAsks() async {
    final body = await _call('GET', '/v1/asks/pending');
    return [for (final a in _list(body, 'asks')) PendingAsk.fromJson(a)];
  }

  Future<PendingAsk> ask(String id) async =>
      PendingAsk.fromJson(_map(await _call('GET', '/v1/asks/$id')));

  Future<String> acceptAsk(String id) async {
    final body = _map(await _call('POST', '/v1/asks/$id/accept'));
    return body['booking_id'] as String? ?? '';
  }

  Future<void> declineAsk(String id) async {
    await _call('POST', '/v1/asks/$id/decline');
  }

  // --------------------------------------------------------------- requests

  Future<List<Place>> places(String q) async {
    final body = await _call('GET', '/v1/places?q=${Uri.encodeQueryComponent(q)}', anonymous: true);
    return [for (final p in _list(body, 'places')) Place.fromJson(p)];
  }

  Future<CreateRequestResult> createRequest({
    required Place origin,
    required Place destination,
    required DateTime windowStart,
    required DateTime windowEnd,
    required int seats,
  }) async {
    final body = _map(await _call('POST', '/v1/requests', body: {
      'origin_lon': origin.lon,
      'origin_lat': origin.lat,
      'destination_lon': destination.lon,
      'destination_lat': destination.lat,
      'origin_label': origin.name,
      'destination_label': destination.name,
      'window_start': windowStart.toUtc().toIso8601String(),
      'window_end': windowEnd.toUtc().toIso8601String(),
      'seats': seats,
    }));
    return CreateRequestResult.fromJson(body);
  }

  Future<List<OpenRequest>> myRequests() async {
    final body = await _call('GET', '/v1/requests/mine');
    return [for (final r in _list(body, 'requests')) OpenRequest.fromJson(r)];
  }

  Future<void> cancelRequest(String id) async {
    await _call('DELETE', '/v1/requests/$id');
  }

  // ------------------------------------------------------------------ roads

  Future<List<RouteOption>> routePreview(LatLon origin, LatLon destination) async {
    final body = await _call('POST', '/v1/routes/preview', body: {
      'origin_lon': origin.lon,
      'origin_lat': origin.lat,
      'destination_lon': destination.lon,
      'destination_lat': destination.lat,
    });
    return [for (final r in _list(body, 'routes')) RouteOption.fromJson(r)];
  }

  Future<PublishResult> publishRoute(RouteDraft draft) async =>
      PublishResult.fromJson(_map(await _call('POST', '/v1/routes', body: draft.toJson())));

  /// The cars on the road now, anonymously, as their trimmed lines.
  Future<List<List<LatLon>>> liveMap() async {
    final body = await _call('GET', '/v1/live/map');
    return [
      for (final t in _list(body, 'trips'))
        decodePolyline6(t['polyline'] as String? ?? ''),
    ];
  }

  // ------------------------------------------------------------------- me

  Future<void> registerPushToken(String token, String platform) async {
    await _call('PUT', '/v1/me/push-token', body: {'token': token, 'platform': platform});
  }

  Future<void> putDriver({
    required String make,
    required String model,
    required String fuel,
    required String plate,
    required String colour,
    int? engineCc,
  }) async {
    await _call('PUT', '/v1/me/driver', body: {
      'make': make,
      'model': model,
      'fuel': fuel,
      'plate': plate,
      'colour': colour,
      'engine_cc': ?engineCc,
    });
  }
}

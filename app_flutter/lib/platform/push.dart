import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;

import '../config.dart';
import '../data/api.dart';

/// The package name this build installs under, as Expo needs it to tell the
/// FCM token it is given from the app that will receive it.
const String _appId = 'gr.carpool.app.flutter';

/// The channel a foreground message is shown on.
const String _channelId = 'asks';

/// Telling this phone about a driver who said yes.
///
/// Push is optional at every step: no EAS project, no google-services.json,
/// no permission, no network — each leaves the app working and the next launch
/// trying again.
class Push {
  Push._();

  static bool _ready = false;
  static void Function(String route)? _open;

  static const FlutterSecureStorage _storage = FlutterSecureStorage();
  static const String _deviceKey = 'carpool_push_device_id';

  static final FlutterLocalNotificationsPlugin _notifications =
      FlutterLocalNotificationsPlugin();

  /// Asks for permission, takes a token, and hands it to the server.
  ///
  /// Called on every launch while signed in, not once at sign-up: a token
  /// changes on reinstall and sometimes on its own, and somebody who denied
  /// notifications and later allowed them never tells the app.
  static Future<void> register(Api api) async {
    if (easProjectId.isEmpty) {
      debugPrint('push is off: this build has no EAS_PROJECT_ID');
      return;
    }
    try {
      await Firebase.initializeApp();
    } catch (error) {
      // No google-services.json, or no Play services: the app installs and
      // runs without notifications rather than dying at launch.
      debugPrint('push is off: Firebase is not configured ($error)');
      return;
    }

    try {
      await _setUpNotifications();

      final settings = await FirebaseMessaging.instance.requestPermission();
      if (settings.authorizationStatus == AuthorizationStatus.denied) {
        debugPrint('push is off: notifications are denied');
        return;
      }

      // push.dart: the token this device's FCM registration carries; Expo's
      // endpoint takes exactly that, and `register()` hands out an installation
      // id instead.
      // ignore: deprecated_member_use
      final fcm = await FirebaseMessaging.instance.getToken();
      if (fcm == null || fcm.isEmpty) {
        debugPrint('push is off: no FCM token yet');
        return;
      }
      await _sendToServer(api, fcm);

      // A rotation is not a rare event over a phone's life, and the server
      // retires the old token when it notices.
      // ignore: deprecated_member_use
      FirebaseMessaging.instance.onTokenRefresh.listen((fresh) {
        _sendToServer(api, fresh).catchError((Object error) {
          debugPrint('push token refresh could not be sent: $error');
        });
      });
    } catch (error) {
      // Every step here can fail for reasons that have nothing to do with
      // the person: Expo's token endpoint needs the network, and credentials
      // are a build-time concern. The next launch tries again.
      debugPrint('push registration failed: $error');
    }
  }

  /// Opens what a tapped notification is about.
  ///
  /// The callback is remembered before Firebase is even asked to exist, so a
  /// tap that launched the app is still acted on once sign-in finishes.
  static void listen(void Function(String route) open) {
    _open = open;
  }

  static Future<void> _setUpNotifications() async {
    if (_ready) return;
    _ready = true;
    const android = AndroidInitializationSettings('ic_launcher');
    await _notifications.initialize(
      settings: const InitializationSettings(android: android),
      // A tap on a notification the app drew itself, while it was open.
      onDidReceiveNotificationResponse: (response) {
        final payload = response.payload;
        if (payload == null || payload.isEmpty) return;
        try {
          final decoded = jsonDecode(payload);
          if (decoded is Map) _openRoute(decoded.cast<String, dynamic>());
        } catch (_) {
          // A payload that is not JSON has no ids in it.
        }
      },
    );
    final androidPlugin = _notifications.resolvePlatformSpecificImplementation<
        AndroidFlutterLocalNotificationsPlugin>();
    await androidPlugin?.createNotificationChannel(const AndroidNotificationChannel(
      _channelId,
      'Ερωτήσεις οδηγών',
      description: 'Ένας οδηγός ρωτά ή απαντά για τη διαδρομή σου.',
      importance: Importance.high,
    ));

    FirebaseMessaging.onMessage.listen(_showForeground);

    // A tap that brought the app back from the background.
    FirebaseMessaging.onMessageOpenedApp.listen((message) => _openRoute(message.data));

    // A tap that launched the app from nothing. Read once: the library keeps
    // it until it is asked for.
    try {
      final initial = await FirebaseMessaging.instance.getInitialMessage();
      if (initial != null) _openRoute(initial.data);
    } catch (error) {
      debugPrint('the launch notification could not be read: $error');
    }
  }

  static void _openRoute(Map<String, dynamic> data) {
    final open = _open;
    if (open == null) return;
    open(_routeOf(data));
  }

  /// Where a notification's payload says to land, checked for shape before it
  /// becomes a path: the payload is data from outside the app.
  static String _routeOf(Map<String, dynamic> data) {
    final fields = _payloadFields(data);
    const id = r'^[0-9a-fA-F-]{1,64}$';
    final ask = fields['ask_id'];
    if (ask is String && RegExp(id).hasMatch(ask)) return '/ask/$ask';
    final occurrence = fields['occurrence_id'];
    if (occurrence is String && RegExp(id).hasMatch(occurrence)) return '/trip/$occurrence';
    return '';
  }

  /// The payload's fields, whichever way Expo packed them.
  ///
  /// Through Expo the server's data arrives as a JSON string under 'body'; a
  /// notification the app itself drew has them at the top level.
  static Map<String, dynamic> _payloadFields(Map<String, dynamic> data) {
    final body = data['body'];
    if (body is String && body.isNotEmpty) {
      try {
        final decoded = jsonDecode(body);
        if (decoded is Map) return decoded.cast<String, dynamic>();
      } catch (_) {
        // Not JSON, so it is somebody's text: no ids in it.
      }
    }
    return data;
  }

  static Future<void> _showForeground(RemoteMessage message) async {
    final notification = message.notification;
    if (notification == null) return;
    await _notifications.show(
      id: DateTime.now().millisecondsSinceEpoch.remainder(1 << 31),
      title: notification.title,
      body: notification.body ?? '',
      notificationDetails: const NotificationDetails(
        android: AndroidNotificationDetails(
          _channelId,
          'Ερωτήσεις οδηγών',
          importance: Importance.high,
          priority: Priority.high,
        ),
      ),
      payload: jsonEncode(message.data),
    );
  }

  static Future<void> _sendToServer(Api api, String fcm) async {
    final deviceId = await _deviceId();
    final response = await http.post(
      Uri.parse('https://exp.host/--/api/v2/push/getExpoPushToken'),
      headers: const {
        'content-type': 'application/json; charset=utf-8',
        'accept': 'application/json',
      },
      body: jsonEncode({
        'type': 'fcm',
        'deviceId': deviceId,
        'development': false,
        'appId': _appId,
        'deviceToken': fcm,
        'projectId': easProjectId,
      }),
    );
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw http.ClientException(
        'Expo push token: ${response.statusCode} ${response.body}',
        response.request?.url,
      );
    }
    final decoded = jsonDecode(response.body);
    if (decoded is! Map) throw const FormatException('that is not an Expo answer');
    final data = decoded['data'];
    final token = data is Map ? data['expoPushToken'] : null;
    if (token is! String || token.isEmpty) {
      throw const FormatException('no expoPushToken in the answer');
    }
    await api.registerPushToken(token, 'android');
  }

  /// A stable id for this installation, so Expo does not mint a new token on
  /// every launch.
  static Future<String> _deviceId() async {
    final stored = await _storage.read(key: _deviceKey);
    if (stored != null && stored.isNotEmpty) return stored;
    final random = Random.secure();
    final buffer = StringBuffer();
    for (var i = 0; i < 16; i++) {
      buffer.write(random.nextInt(256).toRadixString(16).padLeft(2, '0'));
    }
    final id = buffer.toString();
    await _storage.write(key: _deviceKey, value: id);
    return id;
  }
}

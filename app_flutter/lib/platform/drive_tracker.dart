import 'dart:async';
import 'dart:io' show Platform;

import 'package:flutter/foundation.dart';
import 'package:geolocator/geolocator.dart';

import '../core/geo.dart';
import '../data/api.dart';

/// The car's position while somebody is driving, kept going with the screen
/// off.
///
/// The trip screen shares a position only while it is open, which is fine for
/// "where is the car" at a corner and useless for a drive: the driver's phone
/// is in a holder showing a maps app. Here the operating system keeps
/// delivering fixes to a foreground service, which is the blue pill on iOS and
/// the ongoing notification Android insists on. It is started by «Φεύγω» and
/// stopped by «Τέλος», never on its own.
///
/// Only the When In Use permission is asked for: a session the driver starts
/// in the foreground keeps running in the background.
class DriveTracker {
  DriveTracker._();

  static StreamSubscription<Position>? _subscription;

  /// The last fix sent, or null when nothing has been sent for this drive.
  static LatLon? _lastSent;
  static DateTime? _lastSentAt;

  /// Sends once every half minute, or at once when the car has clearly moved.
  /// A waiting passenger needs the car to be somewhere real, not an exact
  /// trace of every metre.
  static const Duration _minGap = Duration(seconds: 30);
  static const double _minMoveM = 150;

  /// Whether this phone is sharing a position for that trip right now.
  static String? _tripId;

  static Future<void> start(Api api, String tripId) async {
    await stop(api, null);

    LocationPermission permission;
    try {
      permission = await Geolocator.checkPermission();
      if (permission == LocationPermission.denied) {
        permission = await Geolocator.requestPermission();
      }
    } catch (error) {
      debugPrint('the drive cannot start: no location permission ($error)');
      return;
    }
    if (permission == LocationPermission.denied ||
        permission == LocationPermission.deniedForever) {
      debugPrint('the drive cannot start: location permission was refused');
      return;
    }

    _tripId = tripId;
    _lastSent = null;
    _lastSentAt = null;

    // Android keeps the fixes coming through a foreground service. iOS has no
    // service to describe: it wants the activity type and the indicator.
    final settings = Platform.isIOS
        ? AppleSettings(
            accuracy: LocationAccuracy.high,
            distanceFilter: 0,
            activityType: ActivityType.automotiveNavigation,
            showBackgroundLocationIndicator: true,
            pauseLocationUpdatesAutomatically: false,
          )
        : AndroidSettings(
            accuracy: LocationAccuracy.high,
            distanceFilter: 0,
            intervalDuration: const Duration(seconds: 30),
            foregroundNotificationConfig: const ForegroundNotificationConfig(
              notificationTitle: 'Μοιράζεσαι τη θέση σου',
              notificationText: 'Οι επιβάτες σου βλέπουν πού είσαι μέχρι το τέλος της διαδρομής.',
              enableWakeLock: true,
              setOngoing: true,
            ),
          );

    _subscription = Geolocator.getPositionStream(locationSettings: settings).listen(
      (position) => _send(api, tripId, position),
      onError: (Object error) => debugPrint('a position could not be read: $error'),
      cancelOnError: false,
    );
  }

  /// Stops sampling and tells the server, which lets the passengers see the
  /// car stop sharing rather than wait on a stale fix.
  static Future<void> stop(Api api, String? tripId) async {
    final current = _tripId;
    _tripId = null;
    _lastSent = null;
    _lastSentAt = null;
    await _subscription?.cancel();
    _subscription = null;
    final id = tripId ?? current;
    if (id == null) return;
    try {
      await api.stopPosition(id);
    } catch (error) {
      // The trip may already be over, from another phone. There is nobody
      // left to tell.
      debugPrint('the drive could not say it ended: $error');
    }
  }

  static Future<void> _send(Api api, String tripId, Position position) async {
    if (_tripId != tripId) return;

    final point = LatLon(position.latitude, position.longitude);
    final now = DateTime.now();
    final lastAt = _lastSentAt;
    final lastPoint = _lastSent;
    final movedFar = lastPoint == null || metersBetween(lastPoint, point) >= _minMoveM;
    final apartEnough = lastAt == null || now.difference(lastAt) >= _minGap;
    if (!movedFar && !apartEnough) return;

    _lastSent = point;
    _lastSentAt = now;

    try {
      await api.putPosition(
        tripId,
        position.latitude,
        position.longitude,
        accuracyM: position.accuracy,
        observedAt: position.timestamp,
      );
    } on ApiException catch (error) {
      // The server will take no more for this trip: «Τέλος» on another phone,
      // the trip called off, or its window closed because nobody said «Τέλος».
      // Then sampling stops here too, rather than running until the battery
      // notices. A dropped connection is not that, and changes nothing.
      if (error.status == 404 || error.status == 409) {
        debugPrint('the drive is over at the server (${error.status})');
        await stop(api, null);
      }
    } catch (error) {
      // Offline: the next fix tries again.
      debugPrint('a position was not sent: $error');
    }
  }
}

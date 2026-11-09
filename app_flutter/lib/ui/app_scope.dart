import 'package:flutter/widgets.dart';

import '../data/api.dart';

/// What every screen shares: the server, the session's two moments, and the
/// one way to open whatever a notification is about.
class AppScope extends InheritedWidget {
  const AppScope({
    super.key,
    required this.api,
    required this.signedIn,
    required this.onSignedIn,
    required this.onSignedOut,
    required this.openRoute,
    required super.child,
  });

  final Api api;

  /// Whether somebody is signed in. Null while the stored tokens are still
  /// being read.
  final bool? signedIn;

  /// A session has started: the app shows the home screen, and this is what a
  /// screen that just signed somebody in calls.
  final void Function() onSignedIn;

  /// The session is over: forget the tokens, show the phone screen.
  final Future<void> Function() onSignedOut;

  /// Opens what a push notification is about, as `'/ask/<id>'` or
  /// `'/trip/<id>'`.
  final void Function(String route) openRoute;

  static AppScope? maybeOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<AppScope>();

  static AppScope of(BuildContext context) {
    final scope = maybeOf(context);
    assert(scope != null, 'no AppScope above this widget');
    return scope!;
  }

  @override
  bool updateShouldNotify(AppScope oldWidget) =>
      api != oldWidget.api ||
      signedIn != oldWidget.signedIn ||
      onSignedIn != oldWidget.onSignedIn ||
      onSignedOut != oldWidget.onSignedOut ||
      openRoute != oldWidget.openRoute;
}

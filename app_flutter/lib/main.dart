import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';

import 'config.dart';
import 'core/athens_time.dart';
import 'data/api.dart';
import 'data/models.dart';
import 'platform/drive_tracker.dart';
import 'platform/push.dart';
import 'ui/app_scope.dart';
import 'ui/ask_screen.dart';
import 'ui/auth_screens.dart';
import 'ui/home_screen.dart';
import 'ui/profile_screen.dart';
import 'ui/publish_screen.dart';
import 'ui/request_screen.dart';
import 'ui/search_screen.dart';
import 'ui/theme.dart';
import 'ui/trip_screen.dart';

/// The one navigator that pushes and pops go through, including the ones a
/// tapped notification starts.
final GlobalKey<NavigatorState> carpoolNavigatorKey = GlobalKey<NavigatorState>();

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  initAthensTime();
  runApp(const CarpoolApp());
}

/// The app's shell: one Api, one session, one navigator.
///
/// Screens are pushed on top of the home screen rather than switched between,
/// so nobody loses their place in the map when a driver is answered.
class CarpoolApp extends StatefulWidget {
  const CarpoolApp({super.key});

  @override
  State<CarpoolApp> createState() => _CarpoolAppState();
}

class _CarpoolAppState extends State<CarpoolApp> {
  late final Api _api;

  /// Null until the stored tokens have been read; then whether a session is on.
  bool? _signedIn;

  /// A notification tapped before sign-in finished. It opens once the home
  /// screen is up, not while the phone screen is still asking for a number.
  String? _pendingRoute;

  @override
  void initState() {
    super.initState();
    _api = Api(
      baseUrl: apiUrl,
      tokens: SecureTokenStore(),
      onSessionEnded: _onSessionEnded,
    );
    // Registered before the session is read: the callback is what a tap needs,
    // and the tap may have launched the app.
    Push.listen(_openRoute);
    _restoreSession();
  }

  Future<void> _restoreSession() async {
    final tokens = await _api.tokens.read();
    if (!mounted) return;
    setState(() => _signedIn = tokens != null);
    if (_signedIn == true) {
      Push.register(_api);
      _openPendingRoute();
    }
  }

  /// The server refused the session. The tokens are already gone, so the only
  /// work left is to forget the pushed screens and ask for a number.
  void _onSessionEnded() {
    // No session, so no position goes anywhere; the service must not run on.
    DriveTracker.stop(_api, null);
    carpoolNavigatorKey.currentState?.popUntil((route) => route.isFirst);
    if (!mounted) return;
    setState(() => _signedIn = false);
  }

  /// Somebody just signed in, from the phone, OTP or sign-up screen.
  void _onSignedIn() {
    carpoolNavigatorKey.currentState?.popUntil((route) => route.isFirst);
    if (!mounted) return;
    setState(() => _signedIn = true);
    // Not awaited: nothing on any screen waits for a push token, and a launch
    // that cannot get one must still open.
    Push.register(_api);
    _openPendingRoute();
  }

  Future<void> _onSignedOut() async {
    final navigator = carpoolNavigatorKey.currentState;
    navigator?.popUntil((route) => route.isFirst);
    // A drive in progress stops sharing the car's position with the person who
    // started it, and says so while the session can still say anything.
    await DriveTracker.stop(_api, null);
    await _api.tokens.clear();
    if (!mounted) return;
    setState(() => _signedIn = false);
  }

  void _openPendingRoute() {
    final route = _pendingRoute;
    if (route == null || _signedIn != true) return;
    _pendingRoute = null;
    _openRoute(route);
  }

  /// Opens `'/ask/<id>'`, `'/trip/<id>'`, or the home screen for anything else.
  void _openRoute(String route) {
    if (_signedIn != true) {
      _pendingRoute = route;
      return;
    }
    final navigator = carpoolNavigatorKey.currentState;
    if (navigator == null) {
      _pendingRoute = route;
      return;
    }
    final ask = RegExp(r'^/ask/([0-9a-fA-F-]{1,64})$').firstMatch(route);
    if (ask != null) {
      _openAsk(ask.group(1)!);
      return;
    }
    final trip = RegExp(r'^/trip/([0-9a-fA-F-]{1,64})$').firstMatch(route);
    if (trip != null) {
      _openTrip(trip.group(1)!);
      return;
    }
    navigator.popUntil((r) => r.isFirst);
  }

  void _openTrip(String id) {
    carpoolNavigatorKey.currentState?.push(MaterialPageRoute<void>(
      builder: (_) => TripScreen(
        api: _api,
        tripId: id,
        onStartDrive: _startDrive,
        onEndDrive: (trip) => DriveTracker.stop(_api, trip.id),
      ),
    ));
  }

  void _openAsk(String id) {
    carpoolNavigatorKey.currentState?.push(MaterialPageRoute<void>(
      builder: (_) => AskScreen(api: _api, askId: id),
    ));
  }

  void _openRequest(String id) {
    carpoolNavigatorKey.currentState?.push(MaterialPageRoute<void>(
      builder: (_) => RequestScreen(api: _api, requestId: id),
    ));
  }

  void _openSearch() {
    carpoolNavigatorKey.currentState?.push(MaterialPageRoute<void>(
      builder: (_) => SearchScreen(api: _api, onRequested: _openRequest),
    ));
  }

  void _openPublish() {
    carpoolNavigatorKey.currentState?.push(MaterialPageRoute<void>(
      builder: (_) => PublishScreen(
        api: _api,
        onPublished: (result) {
          final id = result.firstOccurrenceId;
          if (id != null) _openTrip(id);
        },
      ),
    ));
  }

  void _openProfile() {
    carpoolNavigatorKey.currentState?.push(MaterialPageRoute<void>(
      builder: (_) => ProfileScreen(api: _api, onSignOut: _onSignedOut),
    ));
  }

  /// «Φεύγω» is what starts sharing a position, with the screen off.
  /// Reopening the trip of a drive already being shared changes nothing.
  Future<void> _startDrive(TripDetail trip) async {
    if (DriveTracker.isSharing(trip.id)) return;
    await DriveTracker.start(_api, trip.id);
  }

  @override
  Widget build(BuildContext context) {
    final signedIn = _signedIn;
    return AppScope(
      api: _api,
      signedIn: signedIn,
      onSignedIn: _onSignedIn,
      onSignedOut: _onSignedOut,
      openRoute: _openRoute,
      child: MaterialApp(
        title: 'Carpool',
        theme: carpoolTheme(),
        navigatorKey: carpoolNavigatorKey,
        locale: const Locale('el', 'GR'),
        supportedLocales: const [Locale('el', 'GR'), Locale('en', 'US')],
        localizationsDelegates: const [
          GlobalMaterialLocalizations.delegate,
          GlobalWidgetsLocalizations.delegate,
          GlobalCupertinoLocalizations.delegate,
        ],
        home: signedIn == null
            ? const _Starting()
            : signedIn
                ? HomeScreen(
                    api: _api,
                    onOpenTrip: _openTrip,
                    onOpenAsk: _openAsk,
                    onOpenRequest: _openRequest,
                    onSearch: _openSearch,
                    onPublish: _openPublish,
                    onProfile: _openProfile,
                  )
                : PhoneScreen(api: _api),
      ),
    );
  }
}

/// The moment between opening the app and knowing whether somebody is signed
/// in. Short, and never presented as a question.
class _Starting extends StatelessWidget {
  const _Starting();

  @override
  Widget build(BuildContext context) {
    return const Scaffold(body: Center(child: CircularProgressIndicator()));
  }
}

import '../core/geo.dart';
import '../core/polyline.dart';

double _d(Object? value) => (value as num).toDouble();

double? _dn(Object? value) => value == null ? null : (value as num).toDouble();

DateTime _t(Object? value) => DateTime.parse(value as String).toUtc();

DateTime? _tn(Object? value) => value == null ? null : DateTime.parse(value as String).toUtc();

/// [lon, lat] as the server sends points, or null when it sent nothing.
LatLon? _point(Object? value) {
  if (value is! List || value.length < 2) return null;
  return LatLon(_d(value[1]), _d(value[0]));
}

String? _s(Object? value) => value as String?;

/// One JSON object, with string keys, however the literal that built it was
/// typed.
Map<String, dynamic> _m(Object? value) => Map<String, dynamic>.from(value as Map);

/// The person signed in.
class Me {
  const Me({
    required this.id,
    required this.phone,
    required this.name,
    this.photoUrl,
    this.homeRegionId,
    this.bio,
    this.rating,
  });

  factory Me.fromJson(Map<String, dynamic> json) => Me(
        id: json['id'] as String,
        phone: json['phone'] as String? ?? '',
        name: json['name'] as String? ?? '',
        photoUrl: _s(json['photo_url']),
        homeRegionId: _s(json['home_region_id']),
        bio: _s(json['bio']),
        rating: _dn(json['rating']),
      );

  final String id;
  final String phone;
  final String name;
  final String? photoUrl;
  final String? homeRegionId;
  final String? bio;
  final double? rating;
}

/// A settlement, with the municipality that tells two of the same name apart.
class Place {
  const Place({
    required this.id,
    required this.name,
    required this.area,
    required this.lat,
    required this.lon,
    this.kind = 'town',
  });

  factory Place.fromJson(Map<String, dynamic> json) => Place(
        id: json['id'] as String,
        name: json['name'] as String? ?? '',
        area: json['area'] as String? ?? '',
        lat: _d(json['lat']),
        lon: _d(json['lon']),
        kind: json['kind'] as String? ?? 'town',
      );

  final String id;
  final String name;
  final String area;
  final double lat;
  final double lon;
  final String kind;

  LatLon get point => LatLon(lat, lon);
}

/// Somebody else on a trip: the driver or a passenger.
class Participant {
  const Participant({
    required this.id,
    required this.name,
    this.photoUrl,
    this.rating,
    this.phone,
    this.carModel,
    this.carColor,
    this.carPlate,
    this.pickup,
    this.dropoff,
    this.canRate = false,
    this.rated = false,
  });

  factory Participant.fromJson(Map<String, dynamic> json) => Participant(
        id: json['id'] as String,
        name: json['name'] as String? ?? '',
        photoUrl: _s(json['photo_url']),
        rating: _dn(json['rating']),
        phone: _s(json['phone']),
        carModel: _s(json['car_model']),
        carColor: _s(json['car_color']),
        carPlate: _s(json['car_plate']),
        pickup: _point(json['pickup']),
        dropoff: _point(json['dropoff']),
        canRate: json['can_rate'] as bool? ?? false,
        rated: json['rated'] as bool? ?? false,
      );

  final String id;
  final String name;
  final String? photoUrl;
  final double? rating;
  final String? phone;
  final String? carModel;
  final String? carColor;
  final String? carPlate;
  final LatLon? pickup;
  final LatLon? dropoff;

  /// Whether the caller may still rate this person after the trip.
  final bool canRate;

  /// Whether the caller has already rated them.
  final bool rated;
}

/// One pick-up or drop-off on the road, in road order.
class TripStop {
  const TripStop({
    required this.kind,
    required this.mine,
    this.passengerId,
    this.name,
    this.label,
    this.point,
    this.at,
  });

  factory TripStop.fromJson(Map<String, dynamic> json) => TripStop(
        kind: json['kind'] as String? ?? 'pickup',
        mine: json['mine'] as bool? ?? false,
        passengerId: _s(json['passenger_id']),
        name: _s(json['name']),
        label: _s(json['label']),
        point: _point(json['point']),
        at: _tn(json['at']),
      );

  final String kind;
  final bool mine;
  final String? passengerId;
  final String? name;
  final String? label;
  final LatLon? point;
  final DateTime? at;
}

/// Whether the caller drives this trip or rides in it.
enum TripRole { driver, passenger }

TripRole _role(String? value) => value == 'driver' ? TripRole.driver : TripRole.passenger;

/// A departure as the list screens show it.
class TripSummary {
  const TripSummary({
    required this.id,
    required this.role,
    required this.departEarliest,
    required this.departLatest,
    required this.status,
    required this.routeKm,
    required this.originLabel,
    required this.destinationLabel,
    required this.priceEur,
    required this.seatsFree,
  });

  factory TripSummary.fromJson(Map<String, dynamic> json) => TripSummary(
        id: json['id'] as String,
        role: _role(json['role'] as String?),
        departEarliest: _t(json['depart_earliest']),
        departLatest: _t(json['depart_latest']),
        status: json['status'] as String? ?? '',
        routeKm: _d(json['route_km']),
        originLabel: json['origin_label'] as String? ?? '',
        destinationLabel: json['destination_label'] as String? ?? '',
        priceEur: _d(json['price_eur'] ?? 0),
        seatsFree: (json['seats_free'] as num?)?.toInt() ?? 0,
      );

  final String id;
  final TripRole role;
  final DateTime departEarliest;
  final DateTime departLatest;
  final String status;
  final double routeKm;
  final String originLabel;
  final String destinationLabel;
  final double priceEur;
  final int seatsFree;
}

/// A departure with everything the trip screen needs.
class TripDetail {
  const TripDetail({
    required this.summary,
    required this.routePolyline,
    required this.route,
    required this.driver,
    required this.passengers,
    required this.stops,
    this.routeMin,
    this.bookingId,
    this.confirmedByMe = false,
    this.pickupAt,
    this.driveSetAt,
    this.driveEndedAt,
  });

  factory TripDetail.fromJson(Map<String, dynamic> json) {
    final polyline = json['route_polyline'] as String? ?? '';
    return TripDetail(
      summary: TripSummary.fromJson(json),
      routePolyline: polyline,
      route: decodePolyline6(polyline),
      routeMin: (json['route_min'] as num?)?.toInt(),
      driver: Participant.fromJson(_m(json['driver'])),
      passengers: [
        for (final p in (json['passengers'] as List? ?? const []))
          Participant.fromJson(_m(p)),
      ],
      stops: [
        for (final s in (json['stops'] as List? ?? const []))
          TripStop.fromJson(_m(s)),
      ],
      bookingId: _s(json['booking_id']),
      confirmedByMe: json['confirmed_by_me'] as bool? ?? false,
      pickupAt: _tn(json['pickup_at']),
      driveSetAt: _tn(json['drive_set_at']),
      driveEndedAt: _tn(json['drive_ended_at']),
    );
  }

  final TripSummary summary;
  final String routePolyline;
  final List<LatLon> route;
  final int? routeMin;
  final Participant driver;
  final List<Participant> passengers;
  final List<TripStop> stops;
  final String? bookingId;
  final bool confirmedByMe;
  final DateTime? pickupAt;
  final DateTime? driveSetAt;
  final DateTime? driveEndedAt;

  String get id => summary.id;
  TripRole get role => summary.role;
  DateTime get departEarliest => summary.departEarliest;
  DateTime get departLatest => summary.departLatest;
  String get status => summary.status;
  double get routeKm => summary.routeKm;
  String get originLabel => summary.originLabel;
  String get destinationLabel => summary.destinationLabel;
  double get priceEur => summary.priceEur;
  int get seatsFree => summary.seatsFree;

  /// The caller's own pick-up, if they are a passenger on this trip.
  TripStop? get myPickup {
    for (final stop in stops) {
      if (stop.kind == 'pickup' && stop.mine) return stop;
    }
    return null;
  }

  /// Who the caller is on this trip: the driver's id, or the passenger the
  /// pick-up marked as theirs.
  String? get myId {
    if (role == TripRole.driver) return driver.id;
    for (final stop in stops) {
      if (stop.mine && stop.passengerId != null) return stop.passengerId;
    }
    return null;
  }
}

/// Where somebody on the trip is now.
class LivePosition {
  const LivePosition({
    required this.userId,
    required this.name,
    required this.lat,
    required this.lon,
    required this.at,
  });

  factory LivePosition.fromJson(Map<String, dynamic> json) => LivePosition(
        userId: json['user_id'] as String,
        name: json['name'] as String? ?? '',
        lat: _d(json['lat']),
        lon: _d(json['lon']),
        at: _t(json['observed_at'] ?? json['updated_at']),
      );

  final String userId;
  final String name;
  final double lat;
  final double lon;

  /// When the phone took the fix; when it arrived, if the server has no fix time.
  final DateTime at;

  LatLon get point => LatLon(lat, lon);
}

/// Live sharing for one trip: open between [opens] and [closes].
class LiveState {
  const LiveState({
    required this.opens,
    required this.closes,
    required this.positions,
    this.driverEtaMin,
    this.etaAt,
  });

  factory LiveState.fromJson(Map<String, dynamic> json) => LiveState(
        opens: _t(json['opens']),
        closes: _t(json['closes']),
        positions: [
          for (final p in (json['positions'] as List? ?? const []))
            LivePosition.fromJson(_m(p)),
        ],
        driverEtaMin: (json['driver_eta_min'] as num?)?.toInt(),
        etaAt: _tn(json['eta_at']),
      );

  final DateTime opens;
  final DateTime closes;
  final List<LivePosition> positions;
  final int? driverEtaMin;
  final DateTime? etaAt;
}

/// Somebody asking for a seat on one of the caller's departures.
class PendingAsk {
  const PendingAsk({
    required this.id,
    required this.occurrenceId,
    required this.passengerName,
    required this.passengerRatingCount,
    required this.originLabel,
    required this.destinationLabel,
    required this.detourMin,
    required this.detourKm,
    required this.priceEur,
    required this.seats,
    required this.windowStart,
    required this.windowEnd,
    required this.departEarliest,
    required this.expiresAt,
    this.passengerRating,
  });

  factory PendingAsk.fromJson(Map<String, dynamic> json) => PendingAsk(
        id: json['id'] as String,
        occurrenceId: json['occurrence_id'] as String? ?? '',
        passengerName: json['passenger_name'] as String? ?? '',
        passengerRating: _dn(json['passenger_rating']),
        passengerRatingCount: (json['passenger_rating_count'] as num?)?.toInt() ?? 0,
        originLabel: json['origin_label'] as String? ?? '',
        destinationLabel: json['destination_label'] as String? ?? '',
        detourMin: _d(json['detour_min'] ?? 0),
        detourKm: _d(json['detour_km'] ?? 0),
        priceEur: _d(json['price_eur'] ?? 0),
        seats: (json['seats'] as num?)?.toInt() ?? 1,
        windowStart: _t(json['window_start']),
        windowEnd: _t(json['window_end']),
        departEarliest: _t(json['depart_earliest']),
        expiresAt: _t(json['expires_at']),
      );

  final String id;
  final String occurrenceId;
  final String passengerName;
  final double? passengerRating;
  final int passengerRatingCount;
  final String originLabel;
  final String destinationLabel;
  final double detourMin;
  final double detourKm;
  final double priceEur;
  final int seats;
  final DateTime windowStart;
  final DateTime windowEnd;
  final DateTime departEarliest;
  final DateTime expiresAt;
}

/// A ride the caller asked for and is still waiting on.
class OpenRequest {
  const OpenRequest({
    required this.id,
    required this.originLabel,
    required this.destinationLabel,
    required this.windowStart,
    required this.windowEnd,
    required this.seats,
    required this.status,
    required this.driversAsked,
    required this.askingNow,
  });

  factory OpenRequest.fromJson(Map<String, dynamic> json) => OpenRequest(
        id: json['id'] as String,
        originLabel: json['origin_label'] as String? ?? '',
        destinationLabel: json['destination_label'] as String? ?? '',
        windowStart: _t(json['window_start']),
        windowEnd: _t(json['window_end']),
        seats: (json['seats'] as num?)?.toInt() ?? 1,
        status: json['status'] as String? ?? 'open',
        driversAsked: (json['drivers_asked'] as num?)?.toInt() ?? 0,
        askingNow: json['asking_now'] as bool? ?? false,
      );

  final String id;
  final String originLabel;
  final String destinationLabel;
  final DateTime windowStart;
  final DateTime windowEnd;
  final int seats;
  final String status;
  final int driversAsked;
  final bool askingNow;
}

/// A road the preview offers, with the via that makes it an alternative.
class RouteOption {
  const RouteOption({
    required this.line,
    required this.polyline,
    required this.km,
    required this.minutes,
    this.via,
  });

  factory RouteOption.fromJson(Map<String, dynamic> json) {
    final polyline = json['polyline'] as String? ?? '';
    return RouteOption(
      line: decodePolyline6(polyline),
      polyline: polyline,
      km: _d(json['km'] ?? 0),
      minutes: (json['min'] as num?)?.toInt() ?? 0,
      via: _point(json['via']),
    );
  }

  final List<LatLon> line;
  final String polyline;
  final double km;
  final int minutes;
  final LatLon? via;
}

/// What the publish form holds, ready to send.
class RouteDraft {
  const RouteDraft({
    required this.origin,
    required this.destination,
    required this.seats,
    required this.departEarliest,
    required this.departLatest,
    this.via,
    this.oneoffDate,
    this.weekdays,
  });

  final Place origin;
  final Place destination;
  final LatLon? via;
  final int seats;
  final String? oneoffDate;
  final List<int>? weekdays;
  final String departEarliest;
  final String departLatest;

  Map<String, dynamic> toJson() {
    final recurring = weekdays != null && weekdays!.isNotEmpty;
    return {
      'origin_lon': origin.lon,
      'origin_lat': origin.lat,
      'destination_lon': destination.lon,
      'destination_lat': destination.lat,
      'origin_label': origin.name,
      'destination_label': destination.name,
      'seats': seats,
      'kind': recurring ? 'recurring' : 'oneoff',
      if (recurring) 'weekdays': weekdays,
      if (!recurring && oneoffDate != null) 'oneoff_date': oneoffDate,
      'depart_earliest': departEarliest,
      'depart_latest': departLatest,
      if (via != null) 'via': [via!.lon, via!.lat],
    };
  }
}

/// What a new route became on the server.
class PublishResult {
  const PublishResult({required this.templateId, this.firstOccurrenceId});

  factory PublishResult.fromJson(Map<String, dynamic> json) => PublishResult(
        templateId: json['template_id'] as String,
        firstOccurrenceId: _s(json['first_occurrence_id']),
      );

  final String templateId;
  final String? firstOccurrenceId;
}

/// A link to send somebody, and the token it carries.
class SharedLink {
  const SharedLink({required this.token, required this.url});

  factory SharedLink.fromJson(Map<String, dynamic> json) => SharedLink(
        token: json['token'] as String,
        url: json['url'] as String,
      );

  final String token;
  final String url;
}

/// What became of a seat request.
class CreateRequestResult {
  const CreateRequestResult({required this.requestId, required this.asking, this.askId});

  factory CreateRequestResult.fromJson(Map<String, dynamic> json) => CreateRequestResult(
        requestId: json['request_id'] as String,
        asking: json['asking'] as bool? ?? false,
        askId: _s(json['ask_id']),
      );

  final String requestId;
  final bool asking;
  final String? askId;
}

/// What the OTP step answered: tokens for an account, a signup ticket for a
/// number that is not one yet.
class VerifyResult {
  const VerifyResult({this.accessToken, this.refreshToken, this.signupToken});

  factory VerifyResult.fromJson(Map<String, dynamic> json) => VerifyResult(
        accessToken: _s(json['access_token']),
        refreshToken: _s(json['refresh_token']),
        signupToken: _s(json['signup_token']),
      );

  final String? accessToken;
  final String? refreshToken;
  final String? signupToken;
}

/// Somebody the caller has blocked.
class BlockedUser {
  const BlockedUser({required this.id, required this.name, this.photoUrl});

  factory BlockedUser.fromJson(Map<String, dynamic> json) => BlockedUser(
        id: json['id'] as String,
        name: json['name'] as String? ?? '',
        photoUrl: _s(json['photo_url']),
      );

  final String id;
  final String name;
  final String? photoUrl;
}

/// One line of a trip's chat.
class ChatMessage {
  const ChatMessage({
    required this.id,
    required this.senderId,
    required this.senderName,
    required this.body,
    required this.createdAt,
  });

  factory ChatMessage.fromJson(Map<String, dynamic> json) => ChatMessage(
        id: json['id'] as String,
        senderId: json['sender_id'] as String? ?? '',
        senderName: json['sender_name'] as String? ?? '',
        body: json['body'] as String? ?? '',
        createdAt: _t(json['created_at']),
      );

  final String id;
  final String senderId;
  final String senderName;
  final String body;
  final DateTime createdAt;
}

/// A route the caller published, as their own list shows it.
class MyRoute {
  const MyRoute({
    required this.id,
    required this.originLabel,
    required this.destinationLabel,
    required this.routeKm,
    required this.seats,
    required this.kind,
    required this.weekdays,
    required this.departEarliest,
    required this.departLatest,
    required this.upcomingTrips,
    required this.keptTrips,
    required this.linkOpens,
    required this.nudgeWhenLeaving,
    this.oneoffDate,
  });

  factory MyRoute.fromJson(Map<String, dynamic> json) => MyRoute(
        id: json['id'] as String,
        originLabel: json['origin_label'] as String? ?? '',
        destinationLabel: json['destination_label'] as String? ?? '',
        routeKm: _d(json['route_km'] ?? 0),
        seats: (json['seats'] as num?)?.toInt() ?? 1,
        kind: json['kind'] as String? ?? '',
        weekdays: [
          for (final day in (json['weekdays'] as List? ?? const []))
            if (day is num) day.toInt(),
        ],
        oneoffDate: _s(json['oneoff_date']),
        departEarliest: json['depart_earliest'] as String? ?? '',
        departLatest: json['depart_latest'] as String? ?? '',
        upcomingTrips: (json['upcoming_trips'] as num?)?.toInt() ?? 0,
        keptTrips: (json['kept_trips'] as num?)?.toInt() ?? 0,
        linkOpens: (json['link_opens'] as num?)?.toInt() ?? 0,
        nudgeWhenLeaving: json['nudge_when_leaving'] as bool? ?? false,
      );

  final String id;
  final String originLabel;
  final String destinationLabel;
  final double routeKm;
  final int seats;
  final String kind;

  /// ISO weekdays: 1 is Monday, 7 is Sunday.
  final List<int> weekdays;

  /// The one day of a one-off, as 'YYYY-MM-DD'.
  final String? oneoffDate;
  final String departEarliest;
  final String departLatest;
  final int upcomingTrips;

  /// Trips with passengers already in them; retiring leaves them alone.
  final int keptTrips;
  final int linkOpens;
  final bool nudgeWhenLeaving;

  /// The same route with the nudge switched on or off, for the local echo of
  /// the call that just went out.
  MyRoute withNudge(bool on) => MyRoute(
        id: id,
        originLabel: originLabel,
        destinationLabel: destinationLabel,
        routeKm: routeKm,
        seats: seats,
        kind: kind,
        weekdays: weekdays,
        oneoffDate: oneoffDate,
        departEarliest: departEarliest,
        departLatest: departLatest,
        upcomingTrips: upcomingTrips,
        keptTrips: keptTrips,
        linkOpens: linkOpens,
        nudgeWhenLeaving: on,
      );
}

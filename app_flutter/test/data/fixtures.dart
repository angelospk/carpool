// JSON as the Go API sends it. Field names are the server's, from
// app/src/api/endpoints.ts in the Expo app.

const tripJson = {
  'id': 'occ1',
  'role': 'passenger',
  'depart_earliest': '2026-07-30T04:30:00Z',
  'depart_latest': '2026-07-30T05:00:00Z',
  'status': 'scheduled',
  'route_km': 17.0,
  'route_min': 20,
  'origin_label': 'Αλεξανδρούπολη',
  'destination_label': 'Φέρες',
  'price_eur': 3.5,
  'seats_free': 2,
  'route_polyline': '_dh|lA_suep@?_ibE?_ibE',
  'template_id': 't1',
  'weekday': 4,
  'booking_id': 'b1',
  'ends_at_port': false,
  'confirmed_by_me': false,
  'pickup_at': '2026-07-30T04:38:00Z',
  'drive_set_at': null,
  'driver': {
    'id': 'd1',
    'name': 'Νίκος',
    'photo_url': null,
    'rating': 4.8,
    'facebook_url': null,
    'instagram_url': null,
    'phone': '+306900000031',
    'car_model': 'Toyota Yaris',
    'car_color': 'λευκό',
    'car_plate': 'ΕΒΡ 4521',
  },
  'passengers': [
    {
      'id': 'p1',
      'name': 'Ελένη',
      'photo_url': null,
      'rating': null,
      'facebook_url': null,
      'instagram_url': null,
      'pickup': [25.85, 40.85],
      'dropoff': [26.0, 40.85],
    }
  ],
  'stops': [
    {
      'kind': 'pickup',
      'mine': true,
      'passenger_id': 'p1',
      'name': 'Ελένη',
      'label': 'Πλ. Δημαρχείου',
      'point': [25.85, 40.85],
      'at': '2026-07-30T04:38:00Z',
      'seats': 1,
      'luggage': null,
      'luggage_count': null,
    },
    {
      'kind': 'dropoff',
      'mine': true,
      'passenger_id': 'p1',
      'name': 'Ελένη',
      'label': 'Φέρες, ΚΤΕΛ',
      'point': [26.0, 40.85],
      'at': null,
      'seats': 1,
      'luggage': null,
      'luggage_count': null,
    }
  ],
};

const askJson = {
  'id': 'a1',
  'occurrence_id': 'occ1',
  'passenger_name': 'Μαρία',
  'passenger_photo_url': null,
  'passenger_rating': 4.8,
  'passenger_rating_count': 12,
  'shared_groups': <String>[],
  'luggage': 'cabin',
  'luggage_count': 1,
  'window_start': '2026-07-30T04:30:00Z',
  'window_end': '2026-07-30T05:00:00Z',
  'origin_label': 'Πλ. Δημαρχείου',
  'destination_label': 'Φέρες, ΚΤΕΛ',
  'detour_min': 6.4,
  'detour_km': 2.13,
  'price_eur': 3.5,
  'seats': 1,
  'depart_earliest': '2026-07-30T04:30:00Z',
  'expires_at': '2026-07-30T04:00:00Z',
};

const liveJson = {
  'opens': '2026-07-30T04:00:00Z',
  'closes': '2026-07-30T06:00:00Z',
  'positions': [
    {
      'user_id': 'd1',
      'name': 'Νίκος',
      'lat': 40.851,
      'lon': 25.83,
      'updated_at': '2026-07-30T04:15:00Z',
      'observed_at': '2026-07-30T04:14:50Z',
    }
  ],
  'driver_eta_min': 8,
  'eta_at': '2026-07-30T04:23:00Z',
};

const previewJson = {
  'routes': [
    {'polyline': '_dh|lA_suep@?_ibE?_ibE', 'km': 20.0, 'min': 20, 'via': null},
    {
      'polyline': '_dh|lA_suep@_ibE_ibE~hbE_ibE',
      'km': 26.0,
      'min': 25,
      'via': [25.9, 40.95]
    },
  ],
};

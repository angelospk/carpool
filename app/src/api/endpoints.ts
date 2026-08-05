// Typed wrappers over the REST surface. Keeping the shapes here means a server
// contract change breaks the build rather than a screen at runtime.

import { request, requestRaw } from './client';

export type Region = { id: string; name: string };

export type MyRoute = {
  id: string;
  origin_label: string;
  destination_label: string;
  route_km: number;
  seats: number;
  pricing_mode: string;
  kind: string;
  weekdays: number[];
  oneoff_date?: string;
  depart_earliest: string;
  depart_latest: string;
  upcoming_trips: number;
};

export type Place = {
  id: string;
  kind: 'city' | 'town' | 'village' | 'hamlet';
  name: string;
  /** The municipality, which is what tells two Άγιος Γεώργιος apart. */
  area: string;
  area_en?: string;
  name_en?: string;
  lon: number;
  lat: number;
};
export type MeetingPoint = {
  id: string;
  place_id: string;
  /** bus_station, train_station, port, fuel or parking. */
  kind: string;
  name: string;
  name_en?: string;
  lon: number;
  lat: number;
};

/** A meeting point somebody suggested, and what became of it. */
export type PointProposal = {
  id: string;
  place_id: string;
  place_name: string;
  name?: string;
  kind?: string;
  status: 'pending' | 'approved' | 'rejected';
  reason?: string;
  created_at: string;
};

/** A car in the driver's garage. */
export type Vehicle = {
  id: string;
  /** The label the driver gave it, or the make and model. */
  name: string;
  label: string;
  make: string;
  model: string;
  fuel: string;
  colour: string;
  plate: string;
  /** What contributions are worked out from, and how firm that figure is. */
  consumption_l_100km: number;
  consumption_source: 'dataset' | 'co2' | 'band' | 'legacy';
  /** True when the figure was brought down to the ceiling for its fuel class. */
  consumption_capped: boolean;
  is_default: boolean;
};

export type Me = {
  id: string;
  phone: string;
  name: string;
  photo_url: string | null;
  home_region_id: string;
  women_only: boolean;
  facebook_handle: string | null;
  instagram_handle: string | null;
  facebook_url: string | null;
  instagram_url: string | null;
};

export type TripSummary = {
  id: string;
  role: 'driver' | 'passenger';
  depart_earliest: string;
  depart_latest: string;
  status: string;
  route_km: number;
  origin_label: string;
  destination_label: string;
  price_eur: number;
  seats_free: number;
};

export type Participant = {
  id: string;
  name: string;
  photo_url: string | null;
  rating: number | null;
  /** Where they say they can also be found. Self-declared, never verified. */
  facebook_url: string | null;
  instagram_url: string | null;
  car_model?: string | null;
  car_color?: string | null;
  car_plate?: string | null;
  price_eur?: number | null;
  seats?: number | null;
};

/**
 * Why a seat costs what it costs, as the server worked it out.
 *
 * Every figure comes off the receipt frozen when the seat was booked. Nothing
 * here is recomputed on the phone: a client doing its own arithmetic would
 * disagree with the price beside it the day a fuel figure moved.
 */
export type PriceBreakdownData = {
  v: number;
  basis_v: number;
  consumption_l_100km: number;
  consumption_source: 'dataset' | 'co2' | 'band' | 'legacy';
  fuel: string;
  fuel_price_eur: number;
  region_name?: string;
  price_is_regional: boolean;
  trip_km: number;
  /** The whole trip's fuel, before it was divided. */
  fuel_cost_cents: number;
  /** Every seat the driver offered, plus the driver. */
  divisor: number;
  share_cents: number;
  rounded_cents: number;
  toll_share_cents: number;
  total_cents: number;
  /** The trip was under the driver's own free-below threshold. */
  waived: boolean;
  mode: 'formula' | 'free_contribution' | 'free';
};

export type TripDetail = TripSummary & {
  route_polyline: string;
  driver: Participant;
  passengers: Participant[];
  template_id: string;
  /** ISO weekday of the Athens service date: Monday is 1. */
  weekday: number;
  /** The caller's own booking, absent for the driver. */
  booking_id: string | null;
  /** The route ran into a ferry and was cut at the port. */
  ends_at_port: boolean;
  /** The caller's own seat, explained. Absent for the driver. */
  price_breakdown?: PriceBreakdownData;
};

export type PastTrip = TripSummary & {
  booking_id: string;
  driver_name: string;
};

export type Arrangement = {
  id: string;
  template_id: string;
  weekday: number;
  seats: number;
  price_eur: number;
  status: 'pending' | 'active' | 'declined' | 'cancelled' | 'expired';
  until_date: string;
  driver_id: string;
  passenger_id: string;
  passenger_name: string;
  origin_label: string;
  destination_label: string;
  weeks_booked: number;
};

export type Message = {
  id: string;
  sender_id: string;
  sender_name: string;
  body: string;
  created_at: string;
};

export const api = {
  regions: () => request<{ regions: Region[] }>('/v1/regions', { anonymous: true }),

  /**
   * Settlements matching what is being typed into a pickup field.
   *
   * Anonymous, because signing up asks for a home town before there is a
   * session to ask it with. The list is public data — Greek place names out of
   * OpenStreetMap, embedded in the binary — so there is nothing here that a
   * token would be protecting.
   */
  places: (q: string) =>
    request<{ places: Place[] }>(`/v1/places?q=${encodeURIComponent(q)}`, { anonymous: true }),

  // dev_code comes back only when the server runs with DEV_MODE, where the code
  // is not sent by SMS at all. It lets a tester on a laptop sign in without
  // reading the API's log, which is on another machine. It is absent in every
  // other configuration, so treating it as optional is the whole guard.
  requestOtp: (phone: string) =>
    request<{ dev_code?: string } | void>('/v1/auth/otp/request', {
      method: 'POST',
      anonymous: true,
      body: { phone },
    }),

  /**
   * Proves the number, and answers the only question left: is this an account?
   *
   * A number that already is one comes back with tokens. One that is not comes
   * back with a signup_token — a ten-minute ticket to finish joining with,
   * because the code itself has now been spent and cannot be sent twice.
   */
  verifyOtp: (phone: string, code: string) =>
    request<{
      access_token?: string;
      refresh_token?: string;
      signup_token?: string;
      expires_at?: string;
    }>('/v1/auth/otp/verify', {
      method: 'POST',
      anonymous: true,
      body: { phone, code },
    }),

  completeSignup: (body: {
    signup_token: string;
    name: string;
    /** A settlement id out of the place list. The server reads the region off it. */
    home_place_id: string;
    invite_code?: string;
  }) =>
    request<{ access_token: string; refresh_token: string }>('/v1/auth/signup/complete', {
      method: 'POST',
      anonymous: true,
      body,
    }),

  logout: (refreshToken: string) =>
    request<void>('/v1/auth/logout', { method: 'POST', body: { refresh_token: refreshToken } }),

  me: () => request<Me>('/v1/me'),
  updateMe: (
    patch: Partial<Pick<Me, 'name' | 'women_only' | 'facebook_handle' | 'instagram_handle'>>,
  ) => request<Me>('/v1/me', { method: 'PATCH', body: patch }),

  /** Uploads a photo. The server re-encodes it; this sends the bytes as they are. */
  uploadPhoto: (bytes: Blob) => requestRaw<{ photo_url: string }>('/v1/me/photo', bytes),

  removePhoto: () => request<void>('/v1/me/photo', { method: 'DELETE' }),

  /** Sends a code to a new number and returns the challenge to quote back. */
  requestPhoneChange: (phone: string) =>
    request<{ challenge_id: string; dev_code?: string }>('/v1/me/phone/request', {
      method: 'POST',
      body: { phone },
    }),

  /** Confirms the change. The number comes from the challenge, never from here. */
  confirmPhoneChange: (challengeId: string, code: string) =>
    request<{ access_token: string; refresh_token: string }>('/v1/me/phone/confirm', {
      method: 'POST',
      body: { challenge_id: challengeId, code },
    }),

  /** A driver calling off one departure. Retiring a route is the other act. */
  cancelTrip: (id: string) => request<void>(`/v1/trips/${id}`, { method: 'DELETE' }),

  myRoutes: () => request<{ routes: MyRoute[] }>('/v1/me/routes'),

  /** The garage. Empty until the driver has said what they drive. */
  vehicles: () => request<{ vehicles: Vehicle[] }>('/v1/me/vehicles').then((r) => r.vehicles),

  addVehicle: (body: {
    make: string;
    model: string;
    fuel: string;
    engine_cc?: number;
    co2_g_km?: number;
    colour?: string;
    plate: string;
    label?: string;
  }) => request<Vehicle>('/v1/me/vehicles', { method: 'POST', body }),

  setDefaultVehicle: (id: string) =>
    request<void>(`/v1/me/vehicles/${id}/default`, { method: 'POST' }),

  /** Retires rather than deletes: a past trip still names the car it ran in. */
  retireVehicle: (id: string) => request<void>(`/v1/me/vehicles/${id}`, { method: 'DELETE' }),

  /** Where exactly, once a settlement has been chosen. Often empty: four in
      five settlements have no landmark anybody would recognise. */
  meetingPoints: (placeId: string) =>
    request<{ points: MeetingPoint[] }>(
      `/v1/places/points?place=${encodeURIComponent(placeId)}`,
    ).then((r) => r.points),

  /** Suggests a spot. Nobody else sees it until somebody has looked at it. */
  proposeMeetingPoint: (body: {
    place_id: string;
    name: string;
    kind: string;
    lon: number;
    lat: number;
    /** So a retry after a timeout is not a second suggestion. */
    idempotency_key: string;
  }) => request<PointProposal>('/v1/places/points', { method: 'POST', body }),

  /** Somebody's own suggestions. Deliberately not part of the public list. */
  myMeetingPointProposals: () =>
    request<{ proposals: PointProposal[] }>('/v1/me/meeting-points').then((r) => r.proposals),

  retireRoute: (id: string) => request<void>(`/v1/me/routes/${id}`, { method: 'DELETE' }),

  registerPushToken: (token: string, platform: 'ios' | 'android') =>
    request<void>('/v1/me/push-token', { method: 'PUT', body: { token, platform } }),

  publishRoute: (body: {
    origin_lon: number;
    origin_lat: number;
    destination_lon: number;
    destination_lat: number;
    origin_label: string;
    destination_label: string;
    seats: number;
    kind: 'oneoff' | 'recurring';
    weekdays?: number[];
    oneoff_date?: string;
    depart_earliest: string;
    depart_latest: string;
    /** Omitted means "use my profile defaults". */
    allow_stops?: boolean;
    detour_km?: number;
    /** Which car. Omitted means the default, which is what a driver with one sends. */
    vehicle_id?: string;
  }) => request<{ template_id: string; route_km: number; ends_at_port: boolean }>('/v1/routes', {
      method: 'POST',
      body,
    }),

  createRequest: (body: {
    origin_lon: number;
    origin_lat: number;
    destination_lon: number;
    destination_lat: number;
    origin_label: string;
    destination_label: string;
    window_start: string;
    window_end: string;
    seats: number;
  }) =>
    request<{ request_id: string; asking: boolean; ask_id?: string }>('/v1/requests', {
      method: 'POST',
      body,
    }),

  cancelRequest: (id: string) => request<void>(`/v1/requests/${id}`, { method: 'DELETE' }),

  acceptAsk: (id: string) =>
    request<{ booking_id: string }>(`/v1/asks/${id}/accept`, { method: 'POST' }),
  declineAsk: (id: string) => request<void>(`/v1/asks/${id}/decline`, { method: 'POST' }),

  upcomingTrips: () => request<{ trips: TripSummary[] }>('/v1/trips/upcoming'),
  trip: (id: string) => request<TripDetail>(`/v1/trips/${id}`),
  confirmTrip: (id: string) => request<void>(`/v1/trips/${id}/confirm`, { method: 'POST' }),
  rateTrip: (id: string, stars: number, rateeId?: string, tag?: string) =>
    request<void>(`/v1/trips/${id}/rate`, {
      method: 'POST',
      body: { stars, ratee_id: rateeId, tag },
    }),

  messages: (id: string) => request<{ messages: Message[] }>(`/v1/trips/${id}/messages`),
  postMessage: (id: string, body: string) =>
    request<Message>(`/v1/trips/${id}/messages`, { method: 'POST', body: { body } }),

  pastTrips: () => request<{ trips: PastTrip[] }>('/v1/trips/past'),
  again: (bookingId: string) =>
    request<{ request_id: string; ask_id?: string; asking: boolean }>(
      `/v1/bookings/${bookingId}/again`,
      { method: 'POST' },
    ),

  arrangements: () => request<{ arrangements: Arrangement[] }>('/v1/arrangements'),
  repeatBooking: (bookingId: string) =>
    request<{ arrangement_id: string }>(`/v1/bookings/${bookingId}/repeat`, { method: 'POST' }),
  approveArrangement: (id: string) =>
    request<void>(`/v1/arrangements/${id}/approve`, { method: 'POST' }),
  declineArrangement: (id: string) =>
    request<void>(`/v1/arrangements/${id}/decline`, { method: 'POST' }),
  cancelArrangement: (id: string) =>
    request<void>(`/v1/arrangements/${id}`, { method: 'DELETE' }),

  blockUser: (id: string) => request<void>(`/v1/users/${id}/block`, { method: 'POST' }),
  reportUser: (id: string, reason: string) =>
    request<void>(`/v1/users/${id}/report`, { method: 'POST', body: { reason } }),
};

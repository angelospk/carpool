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
  /** How many times a person opened this route's link. */
  link_opens?: number;
  /** Told, as a departure comes up, that somebody on the road is waiting. */
  nudge_when_leaving?: boolean;
};

/** A road somebody is waiting on, and how many people are. Never who. */
export type Corridor = {
  origin_label: string;
  destination_label: string;
  people: number;
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

/**
 * What the driver asks for.
 *
 * `formula` is the app working out the fuel share; `free_contribution` leaves
 * the figure to the two people, and nothing is suggested; `free` is a driver
 * asking for nothing at all.
 */
export type PricingMode = 'formula' | 'free_contribution' | 'free';

/** The closed vocabulary of what a ride is like. Describes; does not enforce. */
export const PREFERENCE_TAGS = [
  'no_smoking',
  'smoking_ok',
  'no_pets',
  'pets_ok',
  'quiet_ride',
  'music_on',
  'chatty',
  'keep_to_myself',
  'air_conditioning',
] as const;

export type PreferenceTag = (typeof PREFERENCE_TAGS)[number];

/**
 * How much somebody is bringing, or how much a driver will take.
 *
 * Ordered, and the order is the whole point: a driver who will take a suitcase
 * will take a cabin bag, and one who will take neither should not be asked
 * about a bicycle. The server excludes on it rather than ranking, so a size
 * chosen here changes who is offered the trip.
 */
export const LUGGAGE_SIZES = ['none', 'cabin', 'suitcase', 'bulky'] as const;

/** A guess made before any driver exists. Never a receipt — see the `kind`. */
export type PlanningEstimate = {
  kind: 'planning_estimate';
  route_km: number;
  route_min: number;
  contribution_cents: number;
  /** The same trip in the thirstiest car the app will price. Zero when there
      is no headroom above the typical figure. */
  ceiling_cents: number;
  assumptions: {
    version: string;
    divisor: number;
    cost_per_km_cents: number;
    fuel_price_as_of?: string;
    region_name?: string;
  };
};

export type LuggageSize = (typeof LUGGAGE_SIZES)[number];

/** The pairs that cannot both be true, so the picker can grey one out. */
export const CONTRADICTIONS: Array<[PreferenceTag, PreferenceTag]> = [
  ['no_smoking', 'smoking_ok'],
  ['no_pets', 'pets_ok'],
  ['quiet_ride', 'music_on'],
  ['chatty', 'keep_to_myself'],
];

export type Me = {
  id: string;
  phone: string;
  name: string;
  photo_url: string | null;
  home_region_id: string;
  /** Asked only for women drivers only; never shown to anybody else. */
  gender?: 'female' | 'male' | 'other' | null;
  women_only: boolean;
  /** A driver not taking asks for now. Absent for somebody with no car. */
  asks_paused?: boolean;
  bio: string | null;
  preferences: PreferenceTag[];
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
  /** Their own words, and what their rides are like. */
  bio?: string | null;
  preferences?: PreferenceTag[];
  /**
   * The groups the caller and this person are both in. Empty for the caller
   * themselves. It answers "why trust this person", so it is only ever
   * reckoned from the caller's side.
   */
  shared_groups?: string[];
  /** Relative to whoever is looking: this person's booking with you is
      complete and you have not rated them yet. */
  can_rate?: boolean;
  /** You have already rated this person for this trip. */
  rated?: boolean;
  /** A passenger's «Είμαι εδώ» on a drive. */
  arrived_at?: string | null;
  /** Where they get in, [lon, lat]: for the driver, and for themselves. */
  pickup?: [number, number] | null;
  /** The seat, for the driver only. */
  booking_id?: string | null;
  /** Where they get out, [lon, lat]: for the driver, and for themselves. */
  dropoff?: [number, number] | null;
};

/** Somebody asking for a seat on one of the caller's departures. */
export type PendingAsk = {
  id: string;
  occurrence_id: string;
  passenger_name: string;
  origin_label: string;
  destination_label: string;
  detour_min: number;
  detour_km: number;
  price_eur: number;
  seats: number;
  depart_earliest: string;
  expires_at: string;
};

/** A route as its link shows it: the share sentence's facts, no driver. */
export type SharedRoute = {
  origin_label: string;
  destination_label: string;
  kind: string;
  weekdays: number[];
  oneoff_date?: string | null;
  depart_earliest: string;
  depart_latest: string;
  seats: number;
  for_group: boolean;
};

/** Where somebody on your trip is, as of updated_at. */
export type LivePosition = {
  user_id: string;
  name: string;
  lat: number;
  lon: number;
  updated_at: string;
};

/** A group the caller belongs to. */
export type Group = {
  id: string;
  name: string;
  role: 'admin' | 'member';
  members: number;
  /** The join code, for admins only. Absent means you may not see it. */
  code?: string;
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
  /** The caller has said the ride happened. */
  confirmed_by_me?: boolean;
  /** The caller's own seat, explained. Absent for the driver. */
  price_breakdown?: PriceBreakdownData;
  /** Set once the driver has said the minute they leave; the window is then
      that minute and scheduled_* is the published one. */
  drive_set_at?: string | null;
  scheduled_earliest?: string | null;
  scheduled_latest?: string | null;
  detour_min?: number;
  /** On a drive, when the car reaches the caller's own pickup. */
  pickup_at?: string | null;
  /** The driver's «Τέλος», and how long the drive takes new passengers. */
  drive_ended_at?: string | null;
  offers_until?: string | null;
  /** End of the driver's road, [lon, lat]. The driver's only. */
  destination?: [number, number] | null;
};

export type PastTrip = TripSummary & {
  booking_id: string;
  driver_name: string;
  /** Where this passenger actually got in and out, off their own booking.
      It is what makes a past ride repeatable: a label names a town, and there
      is no geocoder here to turn one back into a point. */
  origin_lon: number;
  origin_lat: number;
  destination_lon: number;
  destination_lat: number;
};

/**
 * A ride somebody asked for and is still waiting on.
 *
 * Asking is sequential — one driver at a time, each with an expiry — so for
 * minutes at a stretch nothing visible happens. These counts are what let the
 * home screen say which of the several kinds of nothing it is.
 */
export type OpenRequest = {
  id: string;
  origin_label: string;
  destination_label: string;
  window_start: string;
  window_end: string;
  seats: number;
  status: 'open' | 'asking';
  /** People, not attempts: two departures of one driver's route are one driver. */
  drivers_asked: number;
  drivers_declined: number;
  /** Somebody is looking at it this minute. An ask that has lapsed is not. */
  asking_now: boolean;
  /** Absent from a server that predates the field. Absent is not zero. */
  drivers_on_corridor?: number;
  /** The driver took the seat back and the request is being matched again. */
  stranded?: boolean;
  created_at: string;
};

/**
 * A departure the matcher would try next for a request. Anonymous: nobody on
 * it has been asked, so nobody's name is on it.
 */
export type Alternative = {
  depart_earliest: string;
  depart_latest: string;
  origin_label: string;
  destination_label: string;
  seats_free: number;
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
    /** YYYY-MM-DD. The server refuses anybody under eighteen. */
    date_of_birth: string;
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
    patch: Partial<
      Pick<
        Me,
        | 'name'
        | 'gender'
        | 'women_only'
        | 'asks_paused'
        | 'facebook_handle'
        | 'instagram_handle'
        | 'bio'
        | 'preferences'
      >
    >,
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
  /** A passenger gives back their own seat. The trip carries on for the rest. */
  cancelBooking: (bookingId: string) =>
    request<void>(`/v1/bookings/${bookingId}`, { method: 'DELETE' }),

  /**
   * Which roads people are waiting on, for a driver deciding whether to
   * publish. Counts only: the server sends a corridor and a number, and that
   * is the whole of what it is allowed to send.
   */
  demand: () => request<{ corridors: Corridor[]; period_days: number }>('/v1/demand'),

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
  setRouteNudge: (id: string, on: boolean) =>
    request<void>(`/v1/me/routes/${id}/nudge`, { method: 'PUT', body: { on } }),

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
    /** Offer the route to this group's members only. Absent is everybody. */
    group_id?: string;
    /** The largest thing this route will take. Omitted means a suitcase. */
    luggage?: LuggageSize;
    /** How many of that size the boot holds. Absent means three. */
    luggage_capacity?: number;
    /**
     * How much of the computed fuel share you ask for, from just above zero to
     * one. Omitted means all of it.
     *
     * The app works out what the fuel comes to and caps you there; this is you
     * deciding where under that cap you sit.
     */
    contribution_share?: number;
    /**
     * Whether a figure is suggested at all. Omitted means the formula, which
     * is what every route published before the app asked was doing.
     */
    pricing_mode?: PricingMode;
  }) =>
    request<{
      template_id: string;
      route_km: number;
      ends_at_port: boolean;
      /** The first departure, for a trip published to leave on now. */
      first_occurrence_id?: string | null;
    }>('/v1/routes', {
      method: 'POST',
      body,
    }),

  /**
   * Roughly what a seat on a trip like this tends to cost, before any driver
   * has seen it.
   *
   * A POST because it carries coordinates: a query string ends up in proxy
   * logs, analytics and browser history, and a pair of coordinates is where
   * somebody lives and where they go.
   *
   * It is not an offer. Nobody has agreed to anything and no car is known, so
   * the screen showing it has to keep saying so.
   */
  estimate: (body: {
    origin_lon: number;
    origin_lat: number;
    destination_lon: number;
    destination_lat: number;
  }) => request<PlanningEstimate>('/v1/estimates', { method: 'POST', body }),

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
    /** What you are bringing. Omitted means a bag you can hold. */
    luggage?: LuggageSize;
    /** How many of that size you are bringing. Absent means one, or none. */
    luggage_count?: number;
  }) =>
    request<{ request_id: string; asking: boolean; ask_id?: string }>('/v1/requests', {
      method: 'POST',
      body,
    }),

  /** What the caller is still waiting on, and how far the search has got. */
  myRequests: () => request<{ requests: OpenRequest[] }>('/v1/requests/mine'),

  requestAlternatives: (id: string) =>
    request<{ alternatives: Alternative[] }>(`/v1/requests/${id}/alternatives`),

  cancelRequest: (id: string) => request<void>(`/v1/requests/${id}`, { method: 'DELETE' }),

  /** Whether at least three drivers go this way on a day. Never how many. */
  supply: (body: {
    origin_lon: number;
    origin_lat: number;
    destination_lon: number;
    destination_lat: number;
    date: string;
  }) =>
    request<{ status: 'several' | 'unknown' }>('/v1/supply', {
      method: 'POST',
      body,
    }),
  /** The asks waiting on the caller as a driver, soonest to lapse first. */
  pendingAsks: () => request<{ asks: PendingAsk[] }>('/v1/asks/pending'),
  /** One ask, for the driver it was sent to. 404 once it is answered or lapsed. */
  ask: (id: string) => request<PendingAsk>(`/v1/asks/${id}`),
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

  /** When sharing is open for this trip, and where the others you may see are. */
  live: (id: string) =>
    request<{ opens: string; closes: string; positions: LivePosition[] }>(`/v1/trips/${id}/live`),
  /** accuracy_m and observed_at are what let a moving drive take people:
      a fix without them still shows a dot, but times no pickup. */
  putPosition: (
    id: string,
    lat: number,
    lon: number,
    fix?: { accuracy_m?: number | null; observed_at?: string },
  ) => request<void>(`/v1/trips/${id}/position`, { method: 'PUT', body: { lat, lon, ...fix } }),
  /** Pins the minute the driver leaves. A time, not "in five minutes", so a
      retry is the same request. */
  /** On the road: stop or resume taking people, or a new off-road limit. */
  adjustDrive: (id: string, body: { offering?: boolean; detour_min?: number }) =>
    request<void>(`/v1/trips/${id}/drive`, { method: 'PATCH', body }),
  /** «Τέλος». */
  endDrive: (id: string) => request<void>(`/v1/trips/${id}/drive/end`, { method: 'POST' }),
  /** The passenger's «Είμαι εδώ». */
  here: (id: string) => request<void>(`/v1/trips/${id}/here`, { method: 'POST' }),
  startDrive: (id: string, departsAt: string, detourMin?: number) =>
    request<{ departs_at: string }>(`/v1/trips/${id}/drive`, {
      method: 'POST',
      body: { departs_at: departsAt, detour_min: detourMin },
    }),
  stopPosition: (id: string, keepalive = false) =>
    request<void>(`/v1/trips/${id}/position`, { method: 'DELETE', keepalive }),

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

  myGroups: () => request<{ groups: Group[] }>('/v1/groups/mine'),
  createGroup: (name: string) => request<Group>('/v1/groups', { method: 'POST', body: { name } }),
  joinGroup: (code: string) => request<Group>('/v1/groups/join', { method: 'POST', body: { code } }),
  leaveGroup: (id: string) => request<void>(`/v1/groups/${id}/membership`, { method: 'DELETE' }),
  /** The route's link, made the first time and the same one after. */
  shareRouteLink: (id: string) =>
    request<{ token: string; url: string }>(`/v1/me/routes/${id}/share`, { method: 'POST' }),
  /** What a route link shows. Anonymous: it is opened before there is an account. */
  sharedRoute: (token: string) =>
    request<SharedRoute>(`/v1/shared-routes/${encodeURIComponent(token)}`, { anonymous: true }),

  /** A month of verified trips between members, as CSV. Admins only. */
  /** This month of a group, counted. Admins only. */
  groupStats: (id: string) =>
    request<{ members: number; drivers: number; trips: number; seats: number }>(
      `/v1/groups/${id}/stats`,
    ),
  groupTripsCSV: (id: string, month: string) =>
    request<string>(`/v1/groups/${id}/trips.csv?month=${encodeURIComponent(month)}`, { text: true }),
  rotateGroupCode: (id: string) =>
    request<{ code: string }>(`/v1/groups/${id}/code`, { method: 'POST' }),

  blockUser: (id: string) => request<void>(`/v1/users/${id}/block`, { method: 'POST' }),
  unblockUser: (id: string) => request<void>(`/v1/users/${id}/block`, { method: 'DELETE' }),
  /** The people the caller blocked. Never who blocked them. */
  myBlocks: () =>
    request<{ blocked: { id: string; name: string; photo_url: string | null }[] }>(
      '/v1/me/blocks',
    ),
  reportUser: (id: string, reason: string) =>
    request<void>(`/v1/users/${id}/report`, { method: 'POST', body: { reason } }),
};

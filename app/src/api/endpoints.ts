// Typed wrappers over the REST surface. Keeping the shapes here means a server
// contract change breaks the build rather than a screen at runtime.

import { request } from './client';

export type Region = { id: string; name: string; fuel_price_eur: number };
export type Me = {
  id: string;
  phone: string;
  name: string;
  photo_url: string | null;
  home_region_id: string;
  women_only: boolean;
  social_url: string | null;
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
  social_url: string | null;
  car_model?: string | null;
  car_color?: string | null;
  car_plate?: string | null;
  price_eur?: number | null;
  seats?: number | null;
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

  verifyOtp: (phone: string, code: string, name: string, regionId: string) =>
    request<{ access_token: string; refresh_token: string }>('/v1/auth/otp/verify', {
      method: 'POST',
      anonymous: true,
      body: { phone, code, name, region_id: regionId },
    }),

  logout: (refreshToken: string) =>
    request<void>('/v1/auth/logout', { method: 'POST', body: { refresh_token: refreshToken } }),

  me: () => request<Me>('/v1/me'),
  updateMe: (patch: Partial<Pick<Me, 'name' | 'photo_url' | 'women_only' | 'social_url'>>) =>
    request<Me>('/v1/me', { method: 'PATCH', body: patch }),

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
  }) => request<{ template_id: string; route_km: number }>('/v1/routes', { method: 'POST', body }),

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

export interface User {
  id: number;
  username: string;
  name: string;
  avatar_url: string;
  is_superuser: boolean;
  is_active: boolean;
  has_logged_in: boolean;
  last_login_at?: string;
}

export interface AuthProvider {
  /** URL-safe identifier, used in /auth/{name}/login. */
  name: string;
  /** Human-readable name shown on the sign-in button. */
  label: string;
}

export interface Position {
  ts: string;
  lat: number;
  lon: number;
  altitude_ft?: number;
  groundspeed_kt?: number;
  heading_deg?: number;
  /** True for dead-reckoned positions filling an ADS-B coverage gap. */
  is_estimated: boolean;
}

export interface Capabilities {
  /** When true, the Add Flight dialog can drop to "ident + date" only. */
  resolver_available: boolean;
  /** Poll cadence in seconds; drives the "next update in N" footer. */
  poll_interval_sec: number;
  /** When true, the avatar menu shows the "Email addresses…" entry. */
  email_ingest_enabled: boolean;
  /** Forwarding address for email-ingest; absent when disabled. */
  email_ingest_address?: string;
}

export interface UserEmail {
  id: number;
  address: string;
  verified: boolean;
  verified_at?: string;
  created_at: string;
}

export interface ResolveFlightInput {
  ident: string;
  /** YYYY-MM-DD in UTC. */
  date: string;
}

export interface ResolvedFlight {
  ident: string;
  scheduled_out: string;
  scheduled_in: string;
  origin_iata: string;
  origin_lat: number;
  origin_lon: number;
  /** IANA timezone of the origin airport, empty if unknown. */
  origin_tz?: string;
  dest_iata: string;
  dest_lat: number;
  dest_lon: number;
  /** IANA timezone of the destination airport, empty if unknown. */
  dest_tz?: string;
  icao24: string;
  notes: string;
}

export type FlightStatus =
  | 'Scheduled'
  | 'Boarding'
  | 'Departed'
  | 'Enroute'
  | 'Arrived'
  | 'Cancelled'
  | 'Diverted'
  | string;

export interface Flight {
  id: number;
  ident: string;
  icao24?: string;
  scheduled_out: string;
  scheduled_in: string;
  estimated_out?: string;
  estimated_in?: string;
  actual_out?: string;
  actual_in?: string;
  origin_iata: string;
  origin_lat?: number;
  origin_lon?: number;
  /** IANA timezone of the origin airport; used to render scheduled_out
   * in the departure airport's local time. Empty when the IATA is unknown. */
  origin_tz?: string;
  dest_iata: string;
  dest_lat?: number;
  dest_lon?: number;
  /** IANA timezone of the destination airport; used to render scheduled_in
   * and estimated_in in the arrival airport's local time. Empty when the
   * IATA is unknown. */
  dest_tz?: string;
  status: FlightStatus;
  notes: string;
  last_polled_at?: string;
  created_by?: number;
  passenger_ids: number[];
  /** When true, every authenticated user can see the flight, regardless
   * of passenger / share-list membership. */
  is_public: boolean;
  /** Users explicitly granted view access (non-passengers, non-creator). */
  shared_user_ids: number[];
  latest_position?: Position;
  /** Recent positions in order (oldest → newest), for the flown-track line. */
  track?: Position[];
}

export interface CreateFlightInput {
  ident: string;
  scheduled_out: string;
  scheduled_in: string;
  origin_iata: string;
  dest_iata: string;
  icao24?: string;
  notes?: string;
  passenger_ids?: number[];
  shared_user_ids?: number[];
  is_public?: boolean;
}

export interface UpdateFlightInput {
  scheduled_out?: string;
  scheduled_in?: string;
  origin_iata?: string;
  dest_iata?: string;
  icao24?: string;
  notes?: string;
  status?: FlightStatus;
  is_public?: boolean;
}

export interface InviteUserInput {
  username: string;
  name?: string;
  is_superuser?: boolean;
}

export interface UpdateUserInput {
  name?: string;
  is_superuser?: boolean;
  is_active?: boolean;
}

export type FriendshipStatus = 'pending' | 'accepted';

/** Direction is "" (empty) for accepted edges; the API leaves the field off
 * for those rows, so the optional/empty mix is intentional. */
export type FriendshipDirection = 'incoming' | 'outgoing' | '';

export interface Friendship {
  /** The other user in the pair. Absent (omitted on the wire) for outgoing
   *  pending invites — the inviter must not learn whether the target email
   *  belongs to a registered Aerly user. Present otherwise. */
  friend_id?: number;
  /** Inviter-typed email. Present only for outgoing pending invites. */
  email?: string;
  status: FriendshipStatus;
  direction?: FriendshipDirection;
  requested_at: string;
  accepted_at?: string;
}

export interface InviteFriendInput {
  email: string;
  message?: string;
}

export interface Notifications {
  /** Count of friendship rows where the viewer is the recipient and
   *  status is still 'pending'. */
  friend_requests_pending: number;
}

export interface AcceptFriendTokenResult {
  /** Populated when the token resolved to a freshly-accepted row. */
  friendship?: Friendship;
  /** True when the pending row was already gone (already accepted,
   *  cancelled by the inviter, etc.). Mutually exclusive with
   *  `friendship`. */
  already?: boolean;
}

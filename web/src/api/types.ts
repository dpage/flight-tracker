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

// The legacy single-flight model was retired in the trip-planning cut-over.
// The `Flight` shape and its `FlightStatus` survive because the Statistics
// dialog's flown/upcoming rollup (state/stats.ts) still reads /api/flights.
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

// ---------------------------------------------------------------------------
// Trip-planning redesign (Wave 0b scaffold).
//
// These types mirror the LOCKED backend DTO contract (spec §5.3). Field names
// match the JSON wire shape exactly — snake_case as the Go DTOs serialize it,
// same convention as the existing `Flight` type above. The backend agent (0a)
// builds to the same contract; any drift here must be flagged loudly.
// ---------------------------------------------------------------------------

/** The viewer's role on a trip, governing what they can edit. */
export type TripRole = 'owner' | 'editor' | 'viewer';

/** The kind of thing a plan (and each of its parts) represents. Selects which
 * per-type detail object is populated on a `PlanPart`. */
export type PlanType = 'flight' | 'train' | 'hotel' | 'ground' | 'dining' | 'excursion';

/** Lifecycle status of a single plan part. */
export type PlanPartStatus = 'planned' | 'confirmed' | 'cancelled';

/** How a plan's visibility is scoped within its trip.
 * - `everyone`: all trip members see it.
 * - `hidden_from`: all members except `user_ids` see it.
 * - `only_visible_to`: only `user_ids` (plus owner/passengers) see it. */
export type PlanVisibilityMode = 'everyone' | 'hidden_from' | 'only_visible_to';

export interface TripMember {
  user_id: number;
  role: string;
}

export interface Trip {
  id: number;
  name: string;
  destination: string;
  /** YYYY-MM-DD; absent when the trip has no fixed start. */
  starts_on?: string;
  /** YYYY-MM-DD; absent when the trip has no fixed end. */
  ends_on?: string;
  created_by?: number;
  /** The viewer's role on this trip. */
  my_role: TripRole;
  members: TripMember[];
  tags: string[];
  created_at: string;
  updated_at: string;
}

export interface PlanVisibility {
  mode: PlanVisibilityMode;
  user_ids: number[];
}

export interface FlightDetail {
  ident: string;
  icao24?: string;
  callsign: string;
  scheduled_out: string;
  scheduled_in: string;
  estimated_out?: string;
  estimated_in?: string;
  actual_out?: string;
  actual_in?: string;
  origin_iata: string;
  dest_iata: string;
  flight_status: string;
  last_polled_at?: string;
  latest_position?: Position;
  /** Recent positions in order (oldest → newest), for the flown-track line. */
  track?: Position[];
}

export interface HotelDetail {
  property_name: string;
  address: string;
  phone: string;
  room_type: string;
  guests?: number;
  /** Property's standard check-in time of day (HH:MM), if known. */
  standard_checkin?: string;
  /** Property's standard check-out time of day (HH:MM), if known. */
  standard_checkout?: string;
  /** Smart-suggested check-in derived from the surrounding plan (§10). */
  checkin_suggested?: string;
  /** Smart-suggested check-out derived from the surrounding plan (§10). */
  checkout_suggested?: string;
}

export interface TrainDetail {
  operator: string;
  service_no: string;
  coach: string;
  seat: string;
  class: string;
  platform: string;
}

export interface GroundDetail {
  provider: string;
  phone: string;
  vehicle: string;
  driver: string;
  pax?: number;
}

export interface DiningDetail {
  party_size?: number;
  reservation_name: string;
  phone: string;
}

export interface ExcursionDetail {
  provider: string;
  ticket_count?: number;
}

export interface PlanPart {
  id: number;
  plan_id: number;
  type: PlanType;
  seq: number;
  starts_at: string;
  ends_at?: string;
  /** IANA timezone for `starts_at`. */
  start_tz: string;
  /** IANA timezone for `ends_at`. */
  end_tz: string;
  start_label: string;
  start_lat?: number;
  start_lon?: number;
  end_label: string;
  end_lat?: number;
  end_lon?: number;
  status: PlanPartStatus;
  /** Derived COALESCE(actual_*, estimated_*, scheduled_*) used to sort/group
   * every part type uniformly on the timeline. */
  effective_at: string;
  /** Set on the new part of a rebooking; points at the part it replaces. */
  supersedes_id?: number;
  /** When set, the part has been tidied away and drops off the timeline. */
  dismissed_at?: string;
  /** Exactly one of these is populated, selected by `type`. */
  flight?: FlightDetail;
  hotel?: HotelDetail;
  train?: TrainDetail;
  ground?: GroundDetail;
  dining?: DiningDetail;
  excursion?: ExcursionDetail;
}

export interface Plan {
  id: number;
  trip_id: number;
  type: PlanType;
  title: string;
  confirmation_ref: string;
  notes: string;
  source: string;
  created_by?: number;
  passenger_ids: number[];
  visibility: PlanVisibility;
  parts: PlanPart[];
  created_at: string;
  updated_at: string;
}

/** A single trackable part as surfaced by the tracker convergence view. */
export interface TrackerPart {
  plan_part_id: number;
  plan_id: number;
  trip_id: number;
  owner_id?: number;
  title: string;
  status: string;
  effective_at: string;
  ident: string;
  dest_iata: string;
  latest_position?: Position;
}

/** A tag autocomplete candidate from /api/tags/suggest. */
export interface TagSuggestion {
  label: string;
}

// --- Inputs -----------------------------------------------------------------

export interface CreateTripInput {
  name: string;
  destination?: string;
  starts_on?: string;
  ends_on?: string;
}

export interface UpdateTripInput {
  name?: string;
  destination?: string;
  starts_on?: string;
  ends_on?: string;
}

export interface AddTripMemberInput {
  user_id: number;
  role: TripRole;
}

export interface CreatePlanInput {
  type: PlanType;
  title: string;
  confirmation_ref?: string;
  notes?: string;
  passenger_ids?: number[];
  visibility?: PlanVisibility;
  parts: PlanPartInput[];
}

export interface UpdatePlanInput {
  title?: string;
  confirmation_ref?: string;
  notes?: string;
}

/** A part as supplied when creating/editing a plan. */
export interface PlanPartInput {
  type: PlanType;
  seq?: number;
  starts_at: string;
  ends_at?: string;
  start_tz?: string;
  end_tz?: string;
  start_label?: string;
  start_lat?: number;
  start_lon?: number;
  end_label?: string;
  end_lat?: number;
  end_lon?: number;
  status?: PlanPartStatus;
  flight?: Partial<FlightDetail>;
  hotel?: Partial<HotelDetail>;
  train?: Partial<TrainDetail>;
  ground?: Partial<GroundDetail>;
  dining?: Partial<DiningDetail>;
  excursion?: Partial<ExcursionDetail>;
}

export interface UpdatePlanPartInput {
  starts_at?: string;
  ends_at?: string;
  start_tz?: string;
  end_tz?: string;
  start_label?: string;
  start_lat?: number;
  start_lon?: number;
  end_label?: string;
  end_lat?: number;
  end_lon?: number;
  status?: PlanPartStatus;
  flight?: Partial<FlightDetail>;
  hotel?: Partial<HotelDetail>;
  train?: Partial<TrainDetail>;
  ground?: Partial<GroundDetail>;
  dining?: Partial<DiningDetail>;
  excursion?: Partial<ExcursionDetail>;
}

export interface MovePlanInput {
  trip_id: number;
}

/** Source channel for an ingest request. */
export type IngestSource = 'paste' | 'upload' | 'email';

export interface IngestInput {
  /** Pasted text (Manual paste tab). */
  text?: string;
  source?: IngestSource;
}

/** A plan proposed by the ingest pipeline, awaiting confirmation. */
export interface ProposedPlan {
  type: PlanType;
  title: string;
  confirmation_ref: string;
  notes: string;
  /** 0..1 extraction confidence; low values are flagged in the confirm step. */
  confidence: number;
  parts: PlanPart[];
  /** Set when this proposal would supersede an existing part (rebooking). */
  supersedes_part_id?: number;
}

export interface IngestResult {
  proposals: ProposedPlan[];
}

/** A confirmed/edited proposal sent back to /ingest/confirm. */
export interface ConfirmPlanInput {
  type: PlanType;
  title: string;
  confirmation_ref?: string;
  notes?: string;
  passenger_ids?: number[];
  visibility?: PlanVisibility;
  parts: PlanPartInput[];
  supersedes_part_id?: number;
}

export interface IngestConfirmInput {
  plans: ConfirmPlanInput[];
}

/** Scope of an iCal calendar feed token. */
export type CalendarScope = 'me' | 'trip' | 'plan';

export interface CalendarToken {
  scope: CalendarScope;
  token: string;
  /** Ready-to-use feed URL. */
  url: string;
  created_at: string;
}

export interface AlertPrefs {
  in_app: boolean;
  email: boolean;
  /** Suppress flight changes below this many minutes of delay. */
  min_delay_min: number;
}

export interface UpdateAlertPrefsInput {
  in_app?: boolean;
  email?: boolean;
  min_delay_min?: number;
}

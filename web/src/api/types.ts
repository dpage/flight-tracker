export interface User {
  id: number;
  github_login: string;
  name: string;
  avatar_url: string;
  is_superuser: boolean;
  is_active: boolean;
  has_logged_in: boolean;
  last_login_at?: string;
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
  dest_iata: string;
  dest_lat: number;
  dest_lon: number;
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
  dest_iata: string;
  dest_lat?: number;
  dest_lon?: number;
  status: FlightStatus;
  notes: string;
  last_polled_at?: string;
  created_by?: number;
  passenger_ids: number[];
  latest_position?: Position;
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
}

export interface UpdateFlightInput {
  scheduled_out?: string;
  scheduled_in?: string;
  origin_iata?: string;
  dest_iata?: string;
  icao24?: string;
  notes?: string;
  status?: FlightStatus;
}

export interface InviteUserInput {
  github_login: string;
  name?: string;
  is_superuser?: boolean;
}

export interface UpdateUserInput {
  name?: string;
  is_superuser?: boolean;
  is_active?: boolean;
}

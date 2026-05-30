import type {
  AcceptFriendTokenResult,
  AddTripMemberInput,
  AlertPrefs,
  AuthProvider,
  CalendarScope,
  CalendarToken,
  Capabilities,
  ConfirmPlanInput,
  CreatePlanInput,
  CreateTripInput,
  Flight,
  Friendship,
  IngestInput,
  IngestResult,
  InviteFriendInput,
  InviteUserInput,
  MovePlanInput,
  Notifications,
  Plan,
  PlanPart,
  PlanVisibility,
  ResolveFlightInput,
  ResolvedFlight,
  TagSuggestion,
  TrackerPart,
  Trip,
  UpdateAlertPrefsInput,
  UpdatePlanInput,
  UpdatePlanPartInput,
  UpdateTripInput,
  UpdateUserInput,
  User,
  UserEmail,
} from './types';

class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = {
    method,
    credentials: 'include',
    headers: { Accept: 'application/json' },
  };
  if (body !== undefined) {
    init.body = JSON.stringify(body);
    (init.headers as Record<string, string>)['Content-Type'] = 'application/json';
  }
  const res = await fetch(path, init);
  if (res.status === 204) return undefined as T;
  if (!res.ok) {
    let msg = `HTTP ${res.status}`;
    try {
      const j = (await res.json()) as { error?: string };
      if (j.error) msg = j.error;
    } catch {
      // body wasn't JSON; keep status-only message.
    }
    throw new ApiError(res.status, msg);
  }
  return (await res.json()) as T;
}

export const api = {
  isAuthError(err: unknown): boolean {
    return err instanceof ApiError && err.status === 401;
  },

  getMe: () => request<User>('GET', '/api/me'),
  getConfig: () => request<Capabilities>('GET', '/api/config'),

  // Lists the OAuth providers the backend has configured, so the login
  // page can render one button per provider. Returns an empty list on
  // network errors so the page can fall back to the dev-login form.
  // The payload is shape-narrowed before we trust it — a malformed
  // `providers` field (non-array, or entries missing name/label) is
  // treated as empty rather than propagated to consumers.
  async getAuthProviders(): Promise<AuthProvider[]> {
    try {
      const res = await fetch('/auth/providers', {
        credentials: 'include',
        headers: { Accept: 'application/json' },
      });
      if (!res.ok) return [];
      const j = (await res.json()) as { providers?: unknown };
      if (!Array.isArray(j.providers)) return [];
      return j.providers.filter(
        (p): p is AuthProvider =>
          typeof p === 'object' &&
          p !== null &&
          typeof (p as { name?: unknown }).name === 'string' &&
          typeof (p as { label?: unknown }).label === 'string',
      );
    } catch {
      return [];
    }
  },

  // Probes the dev-only DEV_AUTH_BYPASS endpoint. Returns true when the
  // backend is running with DEV_AUTH_BYPASS=1 (the route only exists then),
  // false otherwise (404, network error, non-OK response). The login page
  // uses this to decide whether to render the dev-login form.
  async getDevAuthBypassEnabled(): Promise<boolean> {
    try {
      const res = await fetch('/auth/dev-info', {
        credentials: 'include',
        headers: { Accept: 'application/json' },
      });
      if (!res.ok) return false;
      const j = (await res.json()) as { enabled?: boolean };
      return j.enabled === true;
    } catch {
      return false;
    }
  },

  // The legacy single-flight collection routes were retired in the trip-planning
  // cut-over. Two pieces survive:
  //   - listFlights backs the Statistics dialog's flown/upcoming rollup.
  //   - resolveFlight backs the manual flight-add (ident + date → metadata).
  // The backend keeps exactly these two routes.
  listFlights: (opts?: { showAll?: boolean; showOld?: boolean }) => {
    const params = new URLSearchParams();
    if (opts?.showAll) params.set('show_all', '1');
    if (opts?.showOld) params.set('show_old', '1');
    const qs = params.toString();
    return request<Flight[]>('GET', qs ? `/api/flights?${qs}` : '/api/flights');
  },
  resolveFlight: (input: ResolveFlightInput) =>
    request<ResolvedFlight>('POST', '/api/flights/resolve', input),

  listUsers: () => request<User[]>('GET', '/api/users'),
  inviteUser: (input: InviteUserInput) => request<User>('POST', '/api/users', input),
  updateUser: (id: number, patch: UpdateUserInput) =>
    request<User>('PATCH', `/api/users/${id}`, patch),
  deleteUser: (id: number) => request<void>('DELETE', `/api/users/${id}`),

  listFriends: () => request<Friendship[]>('GET', '/api/friends'),
  // The server returns the same response for "matched an existing user"
  // and "queued an invite to an unknown address" so callers can't enumerate
  // registered users. We expose a single Promise<void> reflecting that.
  inviteFriend: (input: InviteFriendInput) =>
    request<void>('POST', '/api/friends/invite', input).then(() => undefined),
  acceptFriend: (userId: number) => request<Friendship>('POST', `/api/friends/${userId}/accept`),
  removeFriend: (userId: number) => request<void>('DELETE', `/api/friends/${userId}`),
  cancelOutgoingInvite: (email: string) =>
    request<void>('DELETE', '/api/friends/outgoing', { email }).then(() => undefined),
  acceptFriendToken: (token: string) =>
    request<AcceptFriendTokenResult>('POST', '/api/friends/accept-token', { token }),

  getNotifications: () => request<Notifications>('GET', '/api/notifications'),

  listMyEmails: () => request<UserEmail[]>('GET', '/api/me/emails'),
  addMyEmail: (address: string) => request<UserEmail>('POST', '/api/me/emails', { address }),
  resendMyEmail: (id: number) => request<UserEmail>('POST', `/api/me/emails/${id}/resend`),
  deleteMyEmail: (id: number) => request<void>('DELETE', `/api/me/emails/${id}`),

  logout: () =>
    fetch('/auth/logout', { method: 'POST', credentials: 'include' }).then(() => undefined),

  // -------------------------------------------------------------------------
  // Trips (spec §5.2). The list returns my trips plus those shared with me.
  // -------------------------------------------------------------------------
  listTrips: () => request<Trip[]>('GET', '/api/trips'),
  // The single-trip payload carries the timeline data (plans + parts) so the
  // detail view can render without further fetches.
  getTrip: (id: number) => request<Trip & { plans: Plan[] }>('GET', `/api/trips/${id}`),
  createTrip: (input: CreateTripInput) => request<Trip>('POST', '/api/trips', input),
  updateTrip: (id: number, patch: UpdateTripInput) =>
    request<Trip>('PATCH', `/api/trips/${id}`, patch),
  deleteTrip: (id: number) => request<void>('DELETE', `/api/trips/${id}`),
  addTripMember: (tripId: number, input: AddTripMemberInput) =>
    request<Trip>('POST', `/api/trips/${tripId}/members`, input),
  removeTripMember: (tripId: number, userId: number) =>
    request<void>('DELETE', `/api/trips/${tripId}/members/${userId}`),

  // Tags: set the full label list on a trip; suggest autocompletes over the
  // tags the viewer can see.
  setTripTags: (tripId: number, labels: string[]) =>
    request<Trip>('PUT', `/api/trips/${tripId}/tags`, { labels }),
  suggestTags: (q: string) =>
    request<TagSuggestion[]>('GET', `/api/tags/suggest?q=${encodeURIComponent(q)}`),

  // -------------------------------------------------------------------------
  // Plans & parts (spec §5.2).
  // -------------------------------------------------------------------------
  createPlan: (tripId: number, input: CreatePlanInput) =>
    request<Plan>('POST', `/api/trips/${tripId}/plans`, input),
  updatePlan: (id: number, patch: UpdatePlanInput) =>
    request<Plan>('PATCH', `/api/plans/${id}`, patch),
  deletePlan: (id: number) => request<void>('DELETE', `/api/plans/${id}`),
  addPlanPassenger: (planId: number, userId: number) =>
    request<Plan>('POST', `/api/plans/${planId}/passengers`, { user_id: userId }),
  removePlanPassenger: (planId: number, userId: number) =>
    request<void>('DELETE', `/api/plans/${planId}/passengers/${userId}`),
  setPlanVisibility: (planId: number, visibility: PlanVisibility) =>
    request<Plan>('PUT', `/api/plans/${planId}/visibility`, visibility),
  movePlan: (planId: number, input: MovePlanInput) =>
    request<Plan>('POST', `/api/plans/${planId}/move`, input),
  updatePlanPart: (partId: number, patch: UpdatePlanPartInput) =>
    request<PlanPart>('PATCH', `/api/plan-parts/${partId}`, patch),
  // Tidy away a superseded part; stamps dismissed_at so the timeline omits it.
  dismissPlanPart: (partId: number) => request<void>('POST', `/api/plan-parts/${partId}/dismiss`),

  // -------------------------------------------------------------------------
  // Ingest (spec §5.2 / §6): paste/upload → proposed plans, then commit.
  // -------------------------------------------------------------------------
  ingest: (tripId: number, input: IngestInput) =>
    request<IngestResult>('POST', `/api/trips/${tripId}/ingest`, input),
  ingestConfirm: (tripId: number, plans: ConfirmPlanInput[]) =>
    request<Plan[]>('POST', `/api/trips/${tripId}/ingest/confirm`, { plans }),

  // -------------------------------------------------------------------------
  // Calendar tokens (spec §5.2 / §8). The .ics feeds themselves are fetched
  // by external calendar clients via the token URL, not this client.
  // -------------------------------------------------------------------------
  listCalendarTokens: () => request<CalendarToken[]>('GET', '/api/calendar/tokens'),
  // Issue or regenerate (revoking the old one) a token for the given scope.
  issueCalendarToken: (scope: CalendarScope, id?: number) =>
    request<CalendarToken>('POST', '/api/calendar/tokens', { scope, id }),
  revokeCalendarToken: (token: string) =>
    request<void>('DELETE', `/api/calendar/tokens/${encodeURIComponent(token)}`),

  // -------------------------------------------------------------------------
  // Tracker (spec §5.2 / §7): convergence view of trackable parts.
  // -------------------------------------------------------------------------
  getTracker: (opts?: { windowBefore?: string; windowAfter?: string; tag?: string }) => {
    const params = new URLSearchParams();
    if (opts?.windowBefore) params.set('window_before', opts.windowBefore);
    if (opts?.windowAfter) params.set('window_after', opts.windowAfter);
    if (opts?.tag) params.set('tag', opts.tag);
    const qs = params.toString();
    return request<TrackerPart[]>('GET', qs ? `/api/tracker?${qs}` : '/api/tracker');
  },

  // -------------------------------------------------------------------------
  // Alerts (spec §5.2 / §9).
  // -------------------------------------------------------------------------
  getAlertPrefs: () => request<AlertPrefs>('GET', '/api/alert-prefs'),
  updateAlertPrefs: (patch: UpdateAlertPrefsInput) =>
    request<AlertPrefs>('PUT', '/api/alert-prefs', patch),
  // Viewer opt-in to a specific plan's alerts.
  optInPlanAlerts: (planId: number) => request<void>('POST', `/api/plans/${planId}/alerts/optin`),
  optOutPlanAlerts: (planId: number) =>
    request<void>('DELETE', `/api/plans/${planId}/alerts/optin`),
};

export { ApiError };

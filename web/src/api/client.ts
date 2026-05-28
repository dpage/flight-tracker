import type {
  AuthProvider,
  Capabilities,
  CreateFlightInput,
  Flight,
  InviteUserInput,
  ResolveFlightInput,
  ResolvedFlight,
  UpdateFlightInput,
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

  listFlights: (opts?: { showAll?: boolean; showOld?: boolean }) => {
    const params = new URLSearchParams();
    if (opts?.showAll) params.set('show_all', '1');
    if (opts?.showOld) params.set('show_old', '1');
    const qs = params.toString();
    return request<Flight[]>('GET', qs ? `/api/flights?${qs}` : '/api/flights');
  },
  getFlight: (id: number) => request<Flight>('GET', `/api/flights/${id}`),
  createFlight: (input: CreateFlightInput) => request<Flight>('POST', '/api/flights', input),
  resolveFlight: (input: ResolveFlightInput) =>
    request<ResolvedFlight>('POST', '/api/flights/resolve', input),
  updateFlight: (id: number, patch: UpdateFlightInput) =>
    request<Flight>('PATCH', `/api/flights/${id}`, patch),
  deleteFlight: (id: number) => request<void>('DELETE', `/api/flights/${id}`),
  addPassenger: (flightId: number, userId: number) =>
    request<void>('POST', `/api/flights/${flightId}/passengers`, { user_id: userId }),
  removePassenger: (flightId: number, userId: number) =>
    request<void>('DELETE', `/api/flights/${flightId}/passengers/${userId}`),
  addShare: (flightId: number, userId: number) =>
    request<void>('POST', `/api/flights/${flightId}/shares`, { user_id: userId }),
  removeShare: (flightId: number, userId: number) =>
    request<void>('DELETE', `/api/flights/${flightId}/shares/${userId}`),

  listUsers: () => request<User[]>('GET', '/api/users'),
  inviteUser: (input: InviteUserInput) => request<User>('POST', '/api/users', input),
  updateUser: (id: number, patch: UpdateUserInput) =>
    request<User>('PATCH', `/api/users/${id}`, patch),
  deleteUser: (id: number) => request<void>('DELETE', `/api/users/${id}`),

  listMyEmails: () => request<UserEmail[]>('GET', '/api/me/emails'),
  addMyEmail: (address: string) =>
    request<UserEmail>('POST', '/api/me/emails', { address }),
  resendMyEmail: (id: number) =>
    request<UserEmail>('POST', `/api/me/emails/${id}/resend`),
  deleteMyEmail: (id: number) => request<void>('DELETE', `/api/me/emails/${id}`),

  logout: () =>
    fetch('/auth/logout', { method: 'POST', credentials: 'include' }).then(() => undefined),
};

export { ApiError };

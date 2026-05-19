import type {
  Capabilities,
  CreateFlightInput,
  Flight,
  InviteUserInput,
  UpdateFlightInput,
  UpdateUserInput,
  User,
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

  listFlights: () => request<Flight[]>('GET', '/api/flights'),
  getFlight: (id: number) => request<Flight>('GET', `/api/flights/${id}`),
  createFlight: (input: CreateFlightInput) => request<Flight>('POST', '/api/flights', input),
  updateFlight: (id: number, patch: UpdateFlightInput) =>
    request<Flight>('PATCH', `/api/flights/${id}`, patch),
  deleteFlight: (id: number) => request<void>('DELETE', `/api/flights/${id}`),
  addPassenger: (flightId: number, userId: number) =>
    request<void>('POST', `/api/flights/${flightId}/passengers`, { user_id: userId }),
  removePassenger: (flightId: number, userId: number) =>
    request<void>('DELETE', `/api/flights/${flightId}/passengers/${userId}`),

  listUsers: () => request<User[]>('GET', '/api/users'),
  inviteUser: (input: InviteUserInput) => request<User>('POST', '/api/users', input),
  updateUser: (id: number, patch: UpdateUserInput) =>
    request<User>('PATCH', `/api/users/${id}`, patch),
  deleteUser: (id: number) => request<void>('DELETE', `/api/users/${id}`),

  logout: () =>
    fetch('/auth/logout', { method: 'POST', credentials: 'include' }).then(() => undefined),
};

export { ApiError };

import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';

import { api, ApiError } from './client';

function mockFetch(impl: (path: string, init?: RequestInit) => Response | Promise<Response>) {
  const spy = vi.fn(impl);
  globalThis.fetch = spy as unknown as typeof fetch;
  return spy;
}

function jsonResponse(body: unknown, status = 200): Response {
  return {
    status,
    ok: status >= 200 && status < 300,
    json: async () => body,
  } as unknown as Response;
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('request via api.*', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('returns undefined for 204', async () => {
    mockFetch(() => ({ status: 204, ok: true }) as unknown as Response);
    const out = await api.deleteFlight(7);
    expect(out).toBeUndefined();
  });

  it('throws ApiError with server {error} message on non-ok JSON', async () => {
    mockFetch(() => jsonResponse({ error: 'boom' }, 400));
    await expect(api.listFlights()).rejects.toMatchObject({
      name: 'Error',
      status: 400,
      message: 'boom',
    });
  });

  it('keeps status-only message when non-ok body is not JSON', async () => {
    mockFetch(
      () =>
        ({
          status: 500,
          ok: false,
          json: async () => {
            throw new Error('not json');
          },
        }) as unknown as Response,
    );
    await expect(api.listUsers()).rejects.toMatchObject({ status: 500, message: 'HTTP 500' });
  });

  it('keeps status-only message when JSON has no error field', async () => {
    mockFetch(() => jsonResponse({ nope: true }, 403));
    await expect(api.getMe()).rejects.toMatchObject({ status: 403, message: 'HTTP 403' });
  });

  it('returns parsed JSON on ok', async () => {
    mockFetch(() => jsonResponse([{ id: 1 }]));
    const flights = await api.listFlights();
    expect(flights).toEqual([{ id: 1 }]);
  });

  it('sends a JSON body and Content-Type when a body is provided', async () => {
    const spy = mockFetch(() => jsonResponse({ id: 9 }));
    await api.createFlight({
      ident: 'BA1',
      scheduled_out: 'a',
      scheduled_in: 'b',
      origin_iata: 'LHR',
      dest_iata: 'JFK',
    });
    const [path, init] = spy.mock.calls[0];
    expect(path).toBe('/api/flights');
    expect(init?.method).toBe('POST');
    expect(init?.body).toBe(
      JSON.stringify({
        ident: 'BA1',
        scheduled_out: 'a',
        scheduled_in: 'b',
        origin_iata: 'LHR',
        dest_iata: 'JFK',
      }),
    );
    expect((init?.headers as Record<string, string>)['Content-Type']).toBe('application/json');
  });

  it('omits body and Content-Type when there is no body', async () => {
    const spy = mockFetch(() => jsonResponse({ id: 1 }));
    await api.getFlight(1);
    const [, init] = spy.mock.calls[0];
    expect(init?.body).toBeUndefined();
    expect((init?.headers as Record<string, string>)['Content-Type']).toBeUndefined();
  });
});

describe('api.isAuthError', () => {
  it('true only for ApiError with status 401', () => {
    expect(api.isAuthError(new ApiError(401, 'x'))).toBe(true);
    expect(api.isAuthError(new ApiError(500, 'x'))).toBe(false);
    expect(api.isAuthError(new Error('x'))).toBe(false);
    expect(api.isAuthError('x')).toBe(false);
  });
});

describe('every api.* method calls fetch with the right method/path/body', () => {
  let spy: ReturnType<typeof mockFetch>;
  beforeEach(() => {
    spy = mockFetch(() => jsonResponse({ ok: true }));
  });

  const last = () => spy.mock.calls[spy.mock.calls.length - 1];

  it('getMe', async () => {
    await api.getMe();
    expect(last()[0]).toBe('/api/me');
    expect(last()[1]?.method).toBe('GET');
  });

  it('getConfig', async () => {
    await api.getConfig();
    expect(last()[0]).toBe('/api/config');
  });

  it('listFlights', async () => {
    await api.listFlights();
    expect(last()[0]).toBe('/api/flights');
  });

  it('listFlights with showAll passes show_all=1', async () => {
    await api.listFlights({ showAll: true });
    expect(last()[0]).toBe('/api/flights?show_all=1');
  });

  it('listFlights with showOld passes show_old=1', async () => {
    await api.listFlights({ showOld: true });
    expect(last()[0]).toBe('/api/flights?show_old=1');
  });

  it('listFlights with both flags combines them', async () => {
    await api.listFlights({ showAll: true, showOld: true });
    // URLSearchParams preserves insertion order: show_all first, show_old second.
    expect(last()[0]).toBe('/api/flights?show_all=1&show_old=1');
  });

  it('getFlight', async () => {
    await api.getFlight(42);
    expect(last()[0]).toBe('/api/flights/42');
  });

  it('createFlight', async () => {
    await api.createFlight({
      ident: 'X',
      scheduled_out: 'a',
      scheduled_in: 'b',
      origin_iata: 'A',
      dest_iata: 'B',
    });
    expect(last()[0]).toBe('/api/flights');
    expect(last()[1]?.method).toBe('POST');
  });

  it('resolveFlight', async () => {
    await api.resolveFlight({ ident: 'BA1', date: '2024-01-01' });
    expect(last()[0]).toBe('/api/flights/resolve');
    expect(last()[1]?.method).toBe('POST');
  });

  it('updateFlight', async () => {
    await api.updateFlight(5, { notes: 'hi' });
    expect(last()[0]).toBe('/api/flights/5');
    expect(last()[1]?.method).toBe('PATCH');
  });

  it('deleteFlight', async () => {
    mockFetch(() => ({ status: 204, ok: true }) as unknown as Response);
    await api.deleteFlight(5);
  });

  it('addPassenger', async () => {
    await api.addPassenger(3, 9);
    expect(last()[0]).toBe('/api/flights/3/passengers');
    expect(last()[1]?.method).toBe('POST');
    expect(last()[1]?.body).toBe(JSON.stringify({ user_id: 9 }));
  });

  it('removePassenger', async () => {
    await api.removePassenger(3, 9);
    expect(last()[0]).toBe('/api/flights/3/passengers/9');
    expect(last()[1]?.method).toBe('DELETE');
  });

  it('listUsers', async () => {
    await api.listUsers();
    expect(last()[0]).toBe('/api/users');
  });

  it('inviteUser', async () => {
    await api.inviteUser({ username: 'oct' });
    expect(last()[0]).toBe('/api/users');
    expect(last()[1]?.method).toBe('POST');
  });

  it('updateUser', async () => {
    await api.updateUser(2, { name: 'n' });
    expect(last()[0]).toBe('/api/users/2');
    expect(last()[1]?.method).toBe('PATCH');
  });

  it('deleteUser', async () => {
    mockFetch(() => ({ status: 204, ok: true }) as unknown as Response);
    await api.deleteUser(2);
  });

  it('listMyEmails', async () => {
    await api.listMyEmails();
    expect(last()[0]).toBe('/api/me/emails');
    expect(last()[1]?.method).toBe('GET');
  });

  it('addMyEmail', async () => {
    await api.addMyEmail('alice@example.com');
    expect(last()[0]).toBe('/api/me/emails');
    expect(last()[1]?.method).toBe('POST');
    expect(last()[1]?.body).toBe(JSON.stringify({ address: 'alice@example.com' }));
  });

  it('resendMyEmail', async () => {
    await api.resendMyEmail(7);
    expect(last()[0]).toBe('/api/me/emails/7/resend');
    expect(last()[1]?.method).toBe('POST');
  });

  it('deleteMyEmail', async () => {
    mockFetch(() => ({ status: 204, ok: true }) as unknown as Response);
    await api.deleteMyEmail(7);
  });

  it('logout posts to /auth/logout and resolves undefined', async () => {
    const s = mockFetch(
      () => Promise.resolve({ status: 200, ok: true } as unknown as Response),
    );
    const r = await api.logout();
    expect(r).toBeUndefined();
    expect(s.mock.calls[0][0]).toBe('/auth/logout');
    expect(s.mock.calls[0][1]?.method).toBe('POST');
  });
});

describe('api.getAuthProviders', () => {
  it('returns the providers array on 200', async () => {
    const spy = mockFetch(() =>
      jsonResponse({
        providers: [
          { name: 'github', label: 'GitHub' },
          { name: 'google', label: 'Google' },
        ],
      }),
    );
    await expect(api.getAuthProviders()).resolves.toEqual([
      { name: 'github', label: 'GitHub' },
      { name: 'google', label: 'Google' },
    ]);
    expect(spy.mock.calls[0][0]).toBe('/auth/providers');
  });

  it('returns an empty list on non-ok responses', async () => {
    mockFetch(() => jsonResponse({}, 500));
    await expect(api.getAuthProviders()).resolves.toEqual([]);
  });

  it('returns an empty list when the body lacks providers', async () => {
    mockFetch(() => jsonResponse({}));
    await expect(api.getAuthProviders()).resolves.toEqual([]);
  });

  it('returns an empty list when fetch rejects (network down)', async () => {
    mockFetch(() => Promise.reject(new Error('boom')));
    await expect(api.getAuthProviders()).resolves.toEqual([]);
  });

  it('returns an empty list when providers is not an array', async () => {
    mockFetch(() => jsonResponse({ providers: 'oops' }));
    await expect(api.getAuthProviders()).resolves.toEqual([]);
  });

  it('drops entries that do not match the AuthProvider shape', async () => {
    mockFetch(() =>
      jsonResponse({
        providers: [
          { name: 'github', label: 'GitHub' },
          // missing label
          { name: 'no-label' },
          // wrong types
          { name: 42, label: 'Bad' },
          null,
          'not-an-object',
          { name: 'google', label: 'Google' },
        ],
      }),
    );
    await expect(api.getAuthProviders()).resolves.toEqual([
      { name: 'github', label: 'GitHub' },
      { name: 'google', label: 'Google' },
    ]);
  });
});

describe('api.getDevAuthBypassEnabled', () => {
  it('returns true when /auth/dev-info responds with enabled=true', async () => {
    const spy = mockFetch(() => jsonResponse({ enabled: true }));
    await expect(api.getDevAuthBypassEnabled()).resolves.toBe(true);
    expect(spy.mock.calls[0][0]).toBe('/auth/dev-info');
  });

  it('returns false on 404 (route only registered when DEV_AUTH_BYPASS=1)', async () => {
    mockFetch(() => jsonResponse({}, 404));
    await expect(api.getDevAuthBypassEnabled()).resolves.toBe(false);
  });

  it('returns false when the JSON body lacks enabled=true', async () => {
    mockFetch(() => jsonResponse({ enabled: false }));
    await expect(api.getDevAuthBypassEnabled()).resolves.toBe(false);
  });

  it('returns false when fetch rejects (network down)', async () => {
    mockFetch(() => Promise.reject(new Error('boom')));
    await expect(api.getDevAuthBypassEnabled()).resolves.toBe(false);
  });
});

import { describe, it, expect, beforeEach, vi } from 'vitest';

import { ApiError } from '../api/client';
import type { Flight, User } from '../api/types';

vi.mock('../api/client', async () => {
  const actual = await vi.importActual<typeof import('../api/client')>('../api/client');
  return {
    ApiError: actual.ApiError,
    api: {
      isAuthError: actual.api.isAuthError,
      getMe: vi.fn(),
      getConfig: vi.fn(),
      listFlights: vi.fn(),
      getFlight: vi.fn(),
      createFlight: vi.fn(),
      updateFlight: vi.fn(),
      deleteFlight: vi.fn(),
      addPassenger: vi.fn(),
      removePassenger: vi.fn(),
      listUsers: vi.fn(),
      inviteUser: vi.fn(),
      updateUser: vi.fn(),
      deleteUser: vi.fn(),
      logout: vi.fn(),
    },
  };
});

import { api } from '../api/client';
import { useStore } from './store';

const mockApi = api as unknown as Record<string, ReturnType<typeof vi.fn>>;

function flight(over: Partial<Flight> = {}): Flight {
  return {
    id: 1,
    ident: 'BA1',
    scheduled_out: '2024-01-01T10:00:00Z',
    scheduled_in: '2024-01-01T12:00:00Z',
    origin_iata: 'LHR',
    dest_iata: 'JFK',
    status: 'Scheduled',
    notes: '',
    passenger_ids: [],
    ...over,
  };
}

function user(over: Partial<User> = {}): User {
  return {
    id: 1,
    github_login: 'octocat',
    name: 'Octo',
    avatar_url: '',
    is_superuser: false,
    is_active: true,
    has_logged_in: true,
    ...over,
  };
}

const initialState = useStore.getState();

beforeEach(() => {
  vi.clearAllMocks();
  useStore.setState(
    {
      auth: 'loading',
      me: null,
      capabilities: { resolver_available: false },
      flights: [],
      users: [],
      selectedFlightId: null,
      error: null,
    },
    false,
  );
  // restore action references in case a previous test mutated them
  useStore.setState({ ...initialState }, false);
  useStore.setState(
    {
      auth: 'loading',
      me: null,
      capabilities: { resolver_available: false },
      flights: [],
      users: [],
      selectedFlightId: null,
      error: null,
    },
    false,
  );
});

describe('init', () => {
  it('authenticates and loads data on success', async () => {
    mockApi.getMe.mockResolvedValue(user());
    mockApi.getConfig.mockResolvedValue({ resolver_available: true });
    mockApi.listFlights.mockResolvedValue([flight()]);
    mockApi.listUsers.mockResolvedValue([user()]);
    await useStore.getState().init();
    const s = useStore.getState();
    expect(s.auth).toBe('authenticated');
    expect(s.me?.id).toBe(1);
    expect(s.capabilities.resolver_available).toBe(true);
    expect(s.flights).toHaveLength(1);
    expect(s.users).toHaveLength(1);
  });

  it('goes anonymous on 401 ApiError', async () => {
    mockApi.getMe.mockRejectedValue(new ApiError(401, 'unauth'));
    mockApi.getConfig.mockResolvedValue({ resolver_available: false });
    await useStore.getState().init();
    const s = useStore.getState();
    expect(s.auth).toBe('anonymous');
    expect(s.me).toBeNull();
    expect(s.error).toBeNull();
  });

  it('sets error and goes anonymous on other error', async () => {
    mockApi.getMe.mockRejectedValue(new Error('network down'));
    mockApi.getConfig.mockResolvedValue({ resolver_available: false });
    await useStore.getState().init();
    const s = useStore.getState();
    expect(s.auth).toBe('anonymous');
    expect(s.error).toBe('network down');
  });
});

describe('refreshFlights / refreshUsers error branches', () => {
  it('refreshFlights sets error on failure', async () => {
    mockApi.listFlights.mockRejectedValue(new Error('flights failed'));
    await useStore.getState().refreshFlights();
    expect(useStore.getState().error).toBe('flights failed');
  });

  it('refreshUsers sets error on failure (non-Error -> String())', async () => {
    mockApi.listUsers.mockRejectedValue('strerr');
    await useStore.getState().refreshUsers();
    expect(useStore.getState().error).toBe('strerr');
  });

  it('refreshAll calls both', async () => {
    mockApi.listFlights.mockResolvedValue([flight()]);
    mockApi.listUsers.mockResolvedValue([user()]);
    await useStore.getState().refreshAll();
    expect(useStore.getState().flights).toHaveLength(1);
    expect(useStore.getState().users).toHaveLength(1);
  });
});

describe('flight mutations', () => {
  it('createFlight appends and sorts by scheduled_out', async () => {
    useStore.setState({ flights: [flight({ id: 1, scheduled_out: '2024-01-02T00:00:00Z' })] });
    mockApi.createFlight.mockResolvedValue(
      flight({ id: 2, scheduled_out: '2024-01-01T00:00:00Z' }),
    );
    await useStore.getState().createFlight({
      ident: 'X',
      scheduled_out: 'a',
      scheduled_in: 'b',
      origin_iata: 'A',
      dest_iata: 'B',
    });
    expect(useStore.getState().flights.map((f) => f.id)).toEqual([2, 1]);
  });

  it('updateFlight replaces by id', async () => {
    useStore.setState({ flights: [flight({ id: 1, ident: 'OLD' })] });
    mockApi.updateFlight.mockResolvedValue(flight({ id: 1, ident: 'NEW' }));
    await useStore.getState().updateFlight(1, { notes: 'x' });
    expect(useStore.getState().flights[0].ident).toBe('NEW');
  });

  it('deleteFlight removes and clears selectedFlightId when it matches', async () => {
    useStore.setState({ flights: [flight({ id: 1 })], selectedFlightId: 1 });
    mockApi.deleteFlight.mockResolvedValue(undefined);
    await useStore.getState().deleteFlight(1);
    expect(useStore.getState().flights).toHaveLength(0);
    expect(useStore.getState().selectedFlightId).toBeNull();
  });

  it('deleteFlight keeps a different selectedFlightId', async () => {
    useStore.setState({ flights: [flight({ id: 1 }), flight({ id: 2 })], selectedFlightId: 2 });
    mockApi.deleteFlight.mockResolvedValue(undefined);
    await useStore.getState().deleteFlight(1);
    expect(useStore.getState().selectedFlightId).toBe(2);
  });

  it('addPassenger refetches the flight', async () => {
    useStore.setState({ flights: [flight({ id: 1, passenger_ids: [] })] });
    mockApi.addPassenger.mockResolvedValue(undefined);
    mockApi.getFlight.mockResolvedValue(flight({ id: 1, passenger_ids: [9] }));
    await useStore.getState().addPassenger(1, 9);
    expect(useStore.getState().flights[0].passenger_ids).toEqual([9]);
  });

  it('removePassenger refetches the flight', async () => {
    useStore.setState({ flights: [flight({ id: 1, passenger_ids: [9] })] });
    mockApi.removePassenger.mockResolvedValue(undefined);
    mockApi.getFlight.mockResolvedValue(flight({ id: 1, passenger_ids: [] }));
    await useStore.getState().removePassenger(1, 9);
    expect(useStore.getState().flights[0].passenger_ids).toEqual([]);
  });
});

describe('user mutations', () => {
  it('inviteUser appends and sorts by login (case-insensitive)', async () => {
    useStore.setState({ users: [user({ id: 1, github_login: 'Zed' })] });
    mockApi.inviteUser.mockResolvedValue(user({ id: 2, github_login: 'abc' }));
    await useStore.getState().inviteUser({ github_login: 'abc' });
    expect(useStore.getState().users.map((u) => u.id)).toEqual([2, 1]);
  });

  it('updateUser updates the list and me when it is me', async () => {
    useStore.setState({
      users: [user({ id: 1, name: 'old' })],
      me: user({ id: 1, name: 'old' }),
    });
    mockApi.updateUser.mockResolvedValue(user({ id: 1, name: 'new' }));
    await useStore.getState().updateUser(1, { name: 'new' });
    expect(useStore.getState().users[0].name).toBe('new');
    expect(useStore.getState().me?.name).toBe('new');
  });

  it('updateUser leaves me untouched when updating someone else', async () => {
    useStore.setState({
      users: [user({ id: 2, name: 'old' })],
      me: user({ id: 1, name: 'mine' }),
    });
    mockApi.updateUser.mockResolvedValue(user({ id: 2, name: 'new' }));
    await useStore.getState().updateUser(2, { name: 'new' });
    expect(useStore.getState().me?.name).toBe('mine');
  });

  it('updateUser handles me being null', async () => {
    useStore.setState({ users: [user({ id: 2 })], me: null });
    mockApi.updateUser.mockResolvedValue(user({ id: 2, name: 'new' }));
    await useStore.getState().updateUser(2, { name: 'new' });
    expect(useStore.getState().me).toBeNull();
  });

  it('deleteUser removes the user', async () => {
    useStore.setState({ users: [user({ id: 1 }), user({ id: 2 })] });
    mockApi.deleteUser.mockResolvedValue(undefined);
    await useStore.getState().deleteUser(1);
    expect(useStore.getState().users.map((u) => u.id)).toEqual([2]);
  });
});

describe('logout / selectFlight / applyFlightUpdate / setError', () => {
  it('logout resets the store', async () => {
    useStore.setState({
      me: user(),
      auth: 'authenticated',
      flights: [flight()],
      users: [user()],
      selectedFlightId: 1,
      capabilities: { resolver_available: true },
    });
    mockApi.logout.mockResolvedValue(undefined);
    await useStore.getState().logout();
    const s = useStore.getState();
    expect(s.me).toBeNull();
    expect(s.auth).toBe('anonymous');
    expect(s.flights).toEqual([]);
    expect(s.users).toEqual([]);
    expect(s.selectedFlightId).toBeNull();
    expect(s.capabilities.resolver_available).toBe(false);
  });

  it('selectFlight sets selectedFlightId', () => {
    useStore.getState().selectFlight(5);
    expect(useStore.getState().selectedFlightId).toBe(5);
  });

  it('applyFlightUpdate replaces an existing flight by index', () => {
    useStore.setState({ flights: [flight({ id: 1, ident: 'OLD' })] });
    useStore.getState().applyFlightUpdate(flight({ id: 1, ident: 'NEW' }));
    expect(useStore.getState().flights[0].ident).toBe('NEW');
  });

  it('applyFlightUpdate appends and sorts a new flight', () => {
    useStore.setState({ flights: [flight({ id: 1, scheduled_out: '2024-01-02T00:00:00Z' })] });
    useStore.getState().applyFlightUpdate(flight({ id: 2, scheduled_out: '2024-01-01T00:00:00Z' }));
    expect(useStore.getState().flights.map((f) => f.id)).toEqual([2, 1]);
  });

  it('setError sets and clears the error', () => {
    useStore.getState().setError('oops');
    expect(useStore.getState().error).toBe('oops');
    useStore.getState().setError(null);
    expect(useStore.getState().error).toBeNull();
  });
});

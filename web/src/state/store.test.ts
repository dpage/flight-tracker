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
      addShare: vi.fn(),
      removeShare: vi.fn(),
      listUsers: vi.fn(),
      listFriends: vi.fn(),
      inviteUser: vi.fn(),
      updateUser: vi.fn(),
      deleteUser: vi.fn(),
      getNotifications: vi.fn(),
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
    username: 'octocat',
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
  // Provide safe defaults for API methods called by refreshAll so tests that
  // don't care about a particular slice don't need to set them up explicitly.
  mockApi.listFriends.mockResolvedValue([]);
  useStore.setState(
    {
      auth: 'loading',
      me: null,
      capabilities: { resolver_available: false },
      flights: [],
      users: [],
      friendships: [],
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
      friendships: [],
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
  it('createFlight upserts and sorts by scheduled_out', async () => {
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

  // The server publishes a flight.updated SSE event before it returns the
  // HTTP response; if the SSE arrives first, applyFlightUpdate has already
  // inserted the flight by the time createFlight resolves. createFlight must
  // upsert (not append) so the list doesn't show duplicates.
  it('createFlight is idempotent when the SSE event landed first', async () => {
    const f = flight({ id: 7, ident: 'BA1' });
    useStore.setState({ flights: [f] });
    mockApi.createFlight.mockResolvedValue(f);
    await useStore.getState().createFlight({
      ident: 'BA1',
      scheduled_out: 'a',
      scheduled_in: 'b',
      origin_iata: 'A',
      dest_iata: 'B',
    });
    expect(useStore.getState().flights).toHaveLength(1);
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

  it('addShare refetches the flight after the share is added', async () => {
    useStore.setState({ flights: [flight({ id: 1 })] });
    mockApi.addShare.mockResolvedValue(undefined);
    mockApi.getFlight.mockResolvedValue(flight({ id: 1, ident: 'AFTER-ADD' }));
    await useStore.getState().addShare(1, 42);
    expect(mockApi.addShare).toHaveBeenCalledWith(1, 42);
    expect(useStore.getState().flights[0].ident).toBe('AFTER-ADD');
  });

  it('removeShare refetches the flight after the share is removed', async () => {
    useStore.setState({ flights: [flight({ id: 1 })] });
    mockApi.removeShare.mockResolvedValue(undefined);
    mockApi.getFlight.mockResolvedValue(flight({ id: 1, ident: 'AFTER-REMOVE' }));
    await useStore.getState().removeShare(1, 42);
    expect(mockApi.removeShare).toHaveBeenCalledWith(1, 42);
    expect(useStore.getState().flights[0].ident).toBe('AFTER-REMOVE');
  });
});

describe('setShowAll', () => {
  // Node 25 ships a built-in localStorage global that shadows jsdom's working
  // implementation with a methodless stub; vitest @ Node 22 in CI doesn't hit
  // this. Install a fresh in-memory localStorage on window before each test so
  // the suite is portable across both.
  let store: Record<string, string>;

  beforeEach(() => {
    store = {};
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: (k: string) => (k in store ? store[k] : null),
        setItem: (k: string, v: string) => {
          store[k] = String(v);
        },
        removeItem: (k: string) => {
          delete store[k];
        },
        clear: () => {
          store = {};
        },
        key: (i: number) => Object.keys(store)[i] ?? null,
        get length() {
          return Object.keys(store).length;
        },
      },
    });
  });

  it('persists true to localStorage and refetches flights with showAll', async () => {
    mockApi.listFlights.mockResolvedValue([flight({ id: 99 })]);
    await useStore.getState().setShowAll(true);
    expect(window.localStorage.getItem('ft.show_all')).toBe('1');
    expect(useStore.getState().showAll).toBe(true);
    expect(mockApi.listFlights).toHaveBeenCalledWith({ showAll: true, showOld: false });
    expect(useStore.getState().flights.map((f) => f.id)).toEqual([99]);
  });

  it('persists false by removing the localStorage entry', async () => {
    window.localStorage.setItem('ft.show_all', '1');
    mockApi.listFlights.mockResolvedValue([]);
    await useStore.getState().setShowAll(false);
    expect(window.localStorage.getItem('ft.show_all')).toBeNull();
    expect(useStore.getState().showAll).toBe(false);
    expect(mockApi.listFlights).toHaveBeenCalledWith({ showAll: false, showOld: false });
  });

  // Some privacy modes / SSR shims throw on every localStorage access. The
  // persist helper swallows that so flipping the toggle still updates state
  // and triggers a refetch.
  it('swallows localStorage errors and still updates state', async () => {
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: () => {
          throw new Error('blocked');
        },
        setItem: () => {
          throw new Error('blocked');
        },
        removeItem: () => {
          throw new Error('blocked');
        },
      },
    });
    mockApi.listFlights.mockResolvedValue([]);
    await useStore.getState().setShowAll(true);
    expect(useStore.getState().showAll).toBe(true);
    expect(mockApi.listFlights).toHaveBeenCalledWith({ showAll: true, showOld: false });
  });
});

describe('setShowOld', () => {
  let store: Record<string, string>;

  beforeEach(() => {
    store = {};
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: (k: string) => (k in store ? store[k] : null),
        setItem: (k: string, v: string) => {
          store[k] = String(v);
        },
        removeItem: (k: string) => {
          delete store[k];
        },
        clear: () => {
          store = {};
        },
        key: (i: number) => Object.keys(store)[i] ?? null,
        get length() {
          return Object.keys(store).length;
        },
      },
    });
  });

  it('persists true to localStorage and refetches flights with showOld', async () => {
    mockApi.listFlights.mockResolvedValue([flight({ id: 77 })]);
    await useStore.getState().setShowOld(true);

    expect(useStore.getState().showOld).toBe(true);
    expect(mockApi.listFlights).toHaveBeenCalledWith({ showAll: false, showOld: true });
    expect(window.localStorage.getItem('ft.show_old')).toBe('1');
  });

  it('clears localStorage when set to false', async () => {
    window.localStorage.setItem('ft.show_old', '1');
    mockApi.listFlights.mockResolvedValue([]);
    await useStore.getState().setShowOld(false);

    expect(useStore.getState().showOld).toBe(false);
    expect(mockApi.listFlights).toHaveBeenCalledWith({ showAll: false, showOld: false });
    expect(window.localStorage.getItem('ft.show_old')).toBeNull();
  });

  it('swallows localStorage errors and still updates state', async () => {
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem() {
          throw new Error('blocked');
        },
        setItem() {
          throw new Error('blocked');
        },
        removeItem() {
          throw new Error('blocked');
        },
      },
    });
    mockApi.listFlights.mockResolvedValue([]);
    await useStore.getState().setShowOld(true);
    expect(useStore.getState().showOld).toBe(true);
    await useStore.getState().setShowOld(false);
    expect(useStore.getState().showOld).toBe(false);
  });
});

it('refreshFlights passes both showAll and showOld', async () => {
  useStore.setState({ showAll: true, showOld: true }, false);
  mockApi.listFlights.mockResolvedValue([]);
  await useStore.getState().refreshFlights();
  expect(mockApi.listFlights).toHaveBeenCalledWith({ showAll: true, showOld: true });
});

describe('setShowMineOnly', () => {
  // showMineOnly is persisted with inverted semantics: defaults ON when the
  // key is absent, only an explicit OFF ('0') is written to localStorage.
  let store: Record<string, string>;

  beforeEach(() => {
    store = {};
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: (k: string) => (k in store ? store[k] : null),
        setItem: (k: string, v: string) => {
          store[k] = String(v);
        },
        removeItem: (k: string) => {
          delete store[k];
        },
        clear: () => {
          store = {};
        },
        key: (i: number) => Object.keys(store)[i] ?? null,
        get length() {
          return Object.keys(store).length;
        },
      },
    });
  });

  it('flipping off writes "0" to localStorage and updates state', () => {
    useStore.getState().setShowMineOnly(false);
    expect(useStore.getState().showMineOnly).toBe(false);
    expect(window.localStorage.getItem('ft.show_mine_only')).toBe('0');
  });

  it('flipping back on removes the localStorage key', () => {
    window.localStorage.setItem('ft.show_mine_only', '0');
    useStore.getState().setShowMineOnly(true);
    expect(useStore.getState().showMineOnly).toBe(true);
    expect(window.localStorage.getItem('ft.show_mine_only')).toBeNull();
  });

  it('does not refetch flights — filter is client-side', () => {
    useStore.getState().setShowMineOnly(false);
    expect(mockApi.listFlights).not.toHaveBeenCalled();
  });

  it('swallows localStorage errors and still updates state', () => {
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem() {
          throw new Error('blocked');
        },
        setItem() {
          throw new Error('blocked');
        },
        removeItem() {
          throw new Error('blocked');
        },
      },
    });
    useStore.getState().setShowMineOnly(false);
    expect(useStore.getState().showMineOnly).toBe(false);
    useStore.getState().setShowMineOnly(true);
    expect(useStore.getState().showMineOnly).toBe(true);
  });
});

describe('user mutations', () => {
  it('inviteUser appends and sorts by login (case-insensitive)', async () => {
    useStore.setState({ users: [user({ id: 1, username: 'Zed' })] });
    mockApi.inviteUser.mockResolvedValue(user({ id: 2, username: 'abc' }));
    await useStore.getState().inviteUser({ username: 'abc' });
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

  it('applyFlightDelete removes a flight by id and bumps lastUpdateAt', () => {
    useStore.setState({
      flights: [flight({ id: 1 }), flight({ id: 2 })],
      selectedFlightId: 1,
      lastUpdateAt: null,
    });
    useStore.getState().applyFlightDelete(1);
    expect(useStore.getState().flights.map((f) => f.id)).toEqual([2]);
    // Selection cleared because the deleted flight was the selected one.
    expect(useStore.getState().selectedFlightId).toBeNull();
    expect(useStore.getState().lastUpdateAt).not.toBeNull();
  });

  it('applyFlightDelete leaves selectedFlightId alone when a different flight is removed', () => {
    useStore.setState({
      flights: [flight({ id: 1 }), flight({ id: 2 })],
      selectedFlightId: 2,
    });
    useStore.getState().applyFlightDelete(1);
    expect(useStore.getState().selectedFlightId).toBe(2);
  });

  it('setError sets and clears the error', () => {
    useStore.getState().setError('oops');
    expect(useStore.getState().error).toBe('oops');
    useStore.getState().setError(null);
    expect(useStore.getState().error).toBeNull();
  });
});

describe('notifications + notice slice', () => {
  it('init also fetches notifications and writes them to state', async () => {
    mockApi.getMe.mockResolvedValue(user());
    mockApi.getConfig.mockResolvedValue({ resolver_available: false });
    mockApi.listFlights.mockResolvedValue([]);
    mockApi.listUsers.mockResolvedValue([]);
    mockApi.getNotifications.mockResolvedValue({ friend_requests_pending: 3 });
    await useStore.getState().init();
    expect(useStore.getState().notifications.friend_requests_pending).toBe(3);
  });

  it('refreshNotifications swallows errors and leaves state untouched', async () => {
    useStore.setState({ notifications: { friend_requests_pending: 5 } });
    mockApi.getNotifications.mockRejectedValue(new Error('boom'));
    await useStore.getState().refreshNotifications();
    // Count survives the failure; no error written.
    expect(useStore.getState().notifications.friend_requests_pending).toBe(5);
    expect(useStore.getState().error).toBeNull();
  });

  it('applyNotificationsUpdate overwrites notifications state', () => {
    useStore.setState({ notifications: { friend_requests_pending: 0 } });
    useStore.getState().applyNotificationsUpdate({ friend_requests_pending: 7 });
    expect(useStore.getState().notifications.friend_requests_pending).toBe(7);
  });

  it('setNotice sets and clears the notice', () => {
    useStore.getState().setNotice({ message: 'hi', severity: 'success' });
    expect(useStore.getState().notice).toEqual({ message: 'hi', severity: 'success' });
    useStore.getState().setNotice(null);
    expect(useStore.getState().notice).toBeNull();
  });

  it('logout resets notifications and notice', async () => {
    useStore.setState({
      me: user(),
      auth: 'authenticated',
      notifications: { friend_requests_pending: 4 },
      notice: { message: 'x', severity: 'info' },
    });
    mockApi.logout.mockResolvedValue(undefined);
    await useStore.getState().logout();
    const s = useStore.getState();
    expect(s.notifications.friend_requests_pending).toBe(0);
    expect(s.notice).toBeNull();
  });
});

describe('friendships slice', () => {
  beforeEach(() => {
    mockApi.listFriends.mockReset();
  });

  it('starts empty and refreshFriendships loads from the API', async () => {
    const fixtures = [
      { user_low: 1, user_high: 2, friend_id: 2, status: 'accepted', requested_by: 1 },
      { user_low: 1, user_high: 3, friend_id: 3, status: 'pending', requested_by: 1, direction: 'outgoing' },
    ];
    mockApi.listFriends.mockResolvedValueOnce(fixtures);

    expect(useStore.getState().friendships).toEqual([]);
    await useStore.getState().refreshFriendships();
    expect(useStore.getState().friendships).toEqual(fixtures);
  });

  it('refreshAll() also refreshes friendships', async () => {
    mockApi.listFlights.mockResolvedValue([]);
    mockApi.listUsers.mockResolvedValue([]);
    mockApi.listFriends.mockResolvedValue([
      { user_low: 1, user_high: 2, friend_id: 2, status: 'accepted', requested_by: 1 },
    ]);

    await useStore.getState().refreshAll();
    expect(mockApi.listFriends).toHaveBeenCalled();
    expect(useStore.getState().friendships).toHaveLength(1);
  });
});

import type { StateCreator } from 'zustand';

import { api, ApiError } from '../api/client';
import type {
  Capabilities,
  Friendship,
  InviteUserInput,
  Notifications,
  UpdateUserInput,
  User,
} from '../api/types';
import type { StoreState } from './store';

type AuthStatus = 'loading' | 'anonymous' | 'authenticated';

const SHOW_ALL_KEY = 'ft.show_all';

/** The core slice: auth/me/capabilities, the user + friendship caches, and the
 * notification badge. The trip-planning slices (trips, plans, tracker, …) own
 * the redesigned domain state; this slice holds the cross-cutting session
 * bits every page needs. */
export interface CoreSlice {
  auth: AuthStatus;
  me: User | null;
  capabilities: Capabilities;
  users: User[];
  friendships: Friendship[];
  /** Superuser-only: when true, the SSE stream includes every event regardless
   * of visibility. Persisted to localStorage so it survives reloads.
   * Non-superusers see the flag stay false; the server ignores show_all for
   * them in any case. */
  showAll: boolean;
  error: string | null;
  notifications: Notifications;
  notice: { message: string; severity: 'success' | 'info' } | null;

  init: () => Promise<void>;
  refreshAll: () => Promise<void>;
  refreshUsers: () => Promise<void>;
  refreshFriendships: () => Promise<void>;

  inviteUser: (input: InviteUserInput) => Promise<void>;
  updateUser: (id: number, patch: UpdateUserInput) => Promise<void>;
  deleteUser: (id: number) => Promise<void>;

  logout: () => Promise<void>;
  setShowAll: (v: boolean) => Promise<void>;
  setError: (msg: string | null) => void;
  refreshNotifications: () => Promise<void>;
  applyNotificationsUpdate: (n: Notifications) => void;
  setNotice: (n: { message: string; severity: 'success' | 'info' } | null) => void;
}

function loadShowAll(): boolean {
  try {
    return window.localStorage.getItem(SHOW_ALL_KEY) === '1';
  } catch {
    // SSR / privacy modes that throw on localStorage access — treat as off.
    return false;
  }
}

function persistShowAll(v: boolean): void {
  try {
    if (v) window.localStorage.setItem(SHOW_ALL_KEY, '1');
    else window.localStorage.removeItem(SHOW_ALL_KEY);
  } catch {
    // ignore — best effort
  }
}

export const createCoreSlice: StateCreator<StoreState, [], [], CoreSlice> = (set, get) => ({
  auth: 'loading',
  me: null,
  capabilities: { resolver_available: false, poll_interval_sec: 60, email_ingest_enabled: false },
  users: [],
  friendships: [],
  showAll: loadShowAll(),
  error: null,
  notifications: { friend_requests_pending: 0 },
  notice: null,

  async init() {
    try {
      const [me, capabilities] = await Promise.all([api.getMe(), api.getConfig()]);
      set({ me, capabilities, auth: 'authenticated' });
      await Promise.all([get().refreshAll(), get().refreshNotifications()]);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        set({ me: null, auth: 'anonymous' });
      } else {
        set({ error: errorMessage(err), auth: 'anonymous' });
      }
    }
  },

  async refreshAll() {
    await Promise.all([get().refreshUsers(), get().refreshFriendships()]);
  },

  async refreshUsers() {
    try {
      const users = await api.listUsers();
      set({ users });
    } catch (err) {
      set({ error: errorMessage(err) });
    }
  },

  async refreshFriendships() {
    try {
      const friendships = await api.listFriends();
      set({ friendships });
    } catch (err) {
      set({ error: errorMessage(err) });
    }
  },

  async inviteUser(input) {
    const user = await api.inviteUser(input);
    set((s) => ({ users: [...s.users, user].sort(byLogin) }));
  },
  async updateUser(id, patch) {
    const updated = await api.updateUser(id, patch);
    set((s) => ({
      users: s.users.map((u) => (u.id === id ? updated : u)),
      me: s.me?.id === id ? updated : s.me,
    }));
  },
  async deleteUser(id) {
    await api.deleteUser(id);
    set((s) => ({ users: s.users.filter((u) => u.id !== id) }));
  },

  async logout() {
    await api.logout();
    set({
      me: null,
      auth: 'anonymous',
      users: [],
      capabilities: {
        resolver_available: false,
        poll_interval_sec: 60,
        email_ingest_enabled: false,
      },
      notifications: { friend_requests_pending: 0 },
      notice: null,
    });
  },

  async setShowAll(v) {
    persistShowAll(v);
    set({ showAll: v });
    // The SSE connection is re-established by App.tsx because showAll is in its
    // useEffect dependency list, so the new visibility scope takes effect on
    // the event stream immediately.
  },

  setError(msg) {
    set({ error: msg });
  },

  async refreshNotifications() {
    try {
      const n = await api.getNotifications();
      set({ notifications: n });
    } catch {
      // Non-fatal: stale badge is fine; SSE / next reload will recover.
    }
  },

  applyNotificationsUpdate(n) {
    set({ notifications: n });
  },

  setNotice(n) {
    set({ notice: n });
  },
});

function byLogin(a: User, b: User) {
  return a.username.toLowerCase().localeCompare(b.username.toLowerCase());
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

import { create } from 'zustand';

import { api, ApiError } from '../api/client';
import type {
  CreateFlightInput,
  Flight,
  InviteUserInput,
  UpdateFlightInput,
  UpdateUserInput,
  User,
} from '../api/types';

type AuthStatus = 'loading' | 'anonymous' | 'authenticated';

interface AppState {
  auth: AuthStatus;
  me: User | null;
  flights: Flight[];
  users: User[];
  selectedFlightId: number | null;
  error: string | null;

  init: () => Promise<void>;
  refreshAll: () => Promise<void>;
  refreshFlights: () => Promise<void>;
  refreshUsers: () => Promise<void>;

  createFlight: (input: CreateFlightInput) => Promise<void>;
  updateFlight: (id: number, patch: UpdateFlightInput) => Promise<void>;
  deleteFlight: (id: number) => Promise<void>;
  addPassenger: (flightId: number, userId: number) => Promise<void>;
  removePassenger: (flightId: number, userId: number) => Promise<void>;

  inviteUser: (input: InviteUserInput) => Promise<void>;
  updateUser: (id: number, patch: UpdateUserInput) => Promise<void>;
  deleteUser: (id: number) => Promise<void>;

  logout: () => Promise<void>;
  selectFlight: (id: number | null) => void;
  applyFlightUpdate: (f: Flight) => void;
  setError: (msg: string | null) => void;
}

export const useStore = create<AppState>((set, get) => ({
  auth: 'loading',
  me: null,
  flights: [],
  users: [],
  selectedFlightId: null,
  error: null,

  async init() {
    try {
      const me = await api.getMe();
      set({ me, auth: 'authenticated' });
      await get().refreshAll();
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        set({ me: null, auth: 'anonymous' });
      } else {
        set({ error: errorMessage(err), auth: 'anonymous' });
      }
    }
  },

  async refreshAll() {
    await Promise.all([get().refreshFlights(), get().refreshUsers()]);
  },

  async refreshFlights() {
    try {
      const flights = await api.listFlights();
      set({ flights });
    } catch (err) {
      set({ error: errorMessage(err) });
    }
  },

  async refreshUsers() {
    try {
      const users = await api.listUsers();
      set({ users });
    } catch (err) {
      set({ error: errorMessage(err) });
    }
  },

  async createFlight(input) {
    const flight = await api.createFlight(input);
    set((s) => ({ flights: [...s.flights, flight].sort(byScheduledOut) }));
  },
  async updateFlight(id, patch) {
    const updated = await api.updateFlight(id, patch);
    set((s) => ({ flights: s.flights.map((f) => (f.id === id ? updated : f)) }));
  },
  async deleteFlight(id) {
    await api.deleteFlight(id);
    set((s) => ({
      flights: s.flights.filter((f) => f.id !== id),
      selectedFlightId: s.selectedFlightId === id ? null : s.selectedFlightId,
    }));
  },
  async addPassenger(flightId, userId) {
    await api.addPassenger(flightId, userId);
    const updated = await api.getFlight(flightId);
    set((s) => ({ flights: s.flights.map((f) => (f.id === flightId ? updated : f)) }));
  },
  async removePassenger(flightId, userId) {
    await api.removePassenger(flightId, userId);
    const updated = await api.getFlight(flightId);
    set((s) => ({ flights: s.flights.map((f) => (f.id === flightId ? updated : f)) }));
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
    set({ me: null, auth: 'anonymous', flights: [], users: [], selectedFlightId: null });
  },

  selectFlight(id) {
    set({ selectedFlightId: id });
  },

  applyFlightUpdate(f) {
    set((s) => {
      const idx = s.flights.findIndex((x) => x.id === f.id);
      if (idx === -1) return { flights: [...s.flights, f].sort(byScheduledOut) };
      const next = s.flights.slice();
      next[idx] = f;
      return { flights: next };
    });
  },

  setError(msg) {
    set({ error: msg });
  },
}));

function byScheduledOut(a: Flight, b: Flight) {
  return a.scheduled_out.localeCompare(b.scheduled_out);
}

function byLogin(a: User, b: User) {
  return a.github_login.toLowerCase().localeCompare(b.github_login.toLowerCase());
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

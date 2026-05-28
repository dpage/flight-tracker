import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';

import type { Flight, User } from '../api/types';
import { useStore } from './store';
import { isOld, useVisibleFlights, OLD_TICK_MS } from './visibleFlights';

function flight(over: Partial<Flight> = {}): Flight {
  return {
    id: 1,
    ident: 'BA1',
    scheduled_out: '2024-01-01T08:00:00Z',
    scheduled_in: '2024-01-01T10:00:00Z',
    origin_iata: 'LHR',
    dest_iata: 'JFK',
    status: 'Scheduled',
    notes: '',
    passenger_ids: [],
    is_public: false,
    shared_user_ids: [],
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

const initial = useStore.getState();

beforeEach(() => {
  // Reset to a known baseline: no flights, both filters off, no signed-in
  // user. Tests opt into showMineOnly + me explicitly.
  useStore.setState(
    { ...initial, flights: [], showOld: false, showMineOnly: false, me: null },
    true,
  );
});

describe('isOld', () => {
  const now = Date.parse('2024-01-02T12:00:00Z'); // reference clock
  const dayMs = 24 * 60 * 60 * 1000;

  it('uses actual_in when present', () => {
    const f = flight({ actual_in: '2024-01-01T11:00:00Z' }); // 25h ago
    expect(isOld(f, now)).toBe(true);
  });

  it('falls back to estimated_in when actual_in is missing', () => {
    const f = flight({ estimated_in: '2024-01-01T11:30:00Z' }); // 24.5h ago
    expect(isOld(f, now)).toBe(true);
  });

  it('falls back to scheduled_in when both actual and estimated are missing', () => {
    const f = flight({ scheduled_in: '2024-01-02T00:00:00Z' }); // 12h ago
    expect(isOld(f, now)).toBe(false);
  });

  it('treats a flight at exactly 24h as not-old (matches server >= predicate)', () => {
    const f = flight({ actual_in: new Date(now - dayMs).toISOString() });
    expect(isOld(f, now)).toBe(false);
  });

  it('treats an unparseable timestamp as not-old', () => {
    const f = flight({ scheduled_in: 'not a date' });
    expect(isOld(f, now)).toBe(false);
  });
});

describe('useVisibleFlights', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2024-01-02T12:00:00Z'));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('returns the full list when showOld is true', () => {
    useStore.setState(
      {
        flights: [
          flight({ id: 1, scheduled_in: '2024-01-02T11:30:00Z' }),
          flight({ id: 2, actual_in: '2024-01-01T10:00:00Z' }),
        ],
        showOld: true,
      },
      false,
    );
    const { result } = renderHook(() => useVisibleFlights());
    expect(result.current.map((f) => f.id)).toEqual([1, 2]);
  });

  it('hides old flights when showOld is false', () => {
    useStore.setState(
      {
        flights: [
          flight({ id: 1, scheduled_in: '2024-01-02T11:30:00Z' }),
          flight({ id: 2, actual_in: '2024-01-01T10:00:00Z' }), // 26h ago
        ],
        showOld: false,
      },
      false,
    );
    const { result } = renderHook(() => useVisibleFlights());
    expect(result.current.map((f) => f.id)).toEqual([1]);
  });

  // System clock in this describe is 2024-01-02T12:00:00Z (see inner
  // beforeEach), so the default flight() arrival of 2024-01-01T10:00:00Z
  // is 26h old and gets filtered when showOld=false. Each test below
  // either uses a fresh scheduled_in or sets showOld=true.
  const fresh = '2024-01-02T11:30:00Z';

  it('filters to flights where me is a passenger when showMineOnly is on', () => {
    useStore.setState(
      {
        me: user({ id: 42 }),
        showMineOnly: true,
        flights: [
          flight({ id: 1, passenger_ids: [42], scheduled_in: fresh }),
          flight({ id: 2, passenger_ids: [1, 2], scheduled_in: fresh }),
          flight({ id: 3, passenger_ids: [], scheduled_in: fresh }),
        ],
      },
      false,
    );
    const { result } = renderHook(() => useVisibleFlights());
    expect(result.current.map((f) => f.id)).toEqual([1]);
  });

  it('also includes flights me created even if not on the passenger list', () => {
    useStore.setState(
      {
        me: user({ id: 42 }),
        showMineOnly: true,
        flights: [
          flight({ id: 1, passenger_ids: [], created_by: 42, scheduled_in: fresh }), // mine via creator
          flight({ id: 2, passenger_ids: [42], created_by: 99, scheduled_in: fresh }), // mine via passenger
          flight({ id: 3, passenger_ids: [1], created_by: 99, scheduled_in: fresh }), // not mine
        ],
      },
      false,
    );
    const { result } = renderHook(() => useVisibleFlights());
    expect(result.current.map((f) => f.id)).toEqual([1, 2]);
  });

  it('returns the full list when showMineOnly is off, regardless of me', () => {
    useStore.setState(
      {
        me: user({ id: 42 }),
        showMineOnly: false,
        flights: [
          flight({ id: 1, passenger_ids: [42], scheduled_in: fresh }),
          flight({ id: 2, passenger_ids: [1], scheduled_in: fresh }),
        ],
      },
      false,
    );
    const { result } = renderHook(() => useVisibleFlights());
    expect(result.current.map((f) => f.id)).toEqual([1, 2]);
  });

  it('skips the mine-only filter while me is unknown', () => {
    // Auth still loading / not signed in — applying the filter would hide
    // everything; instead we no-op until me arrives.
    useStore.setState(
      {
        me: null,
        showMineOnly: true,
        flights: [
          flight({ id: 1, passenger_ids: [1], scheduled_in: fresh }),
          flight({ id: 2, passenger_ids: [], scheduled_in: fresh }),
        ],
      },
      false,
    );
    const { result } = renderHook(() => useVisibleFlights());
    expect(result.current.map((f) => f.id)).toEqual([1, 2]);
  });

  it('composes mine-only with the old-flight filter', () => {
    useStore.setState(
      {
        me: user({ id: 42 }),
        showMineOnly: true,
        showOld: false,
        flights: [
          flight({ id: 1, passenger_ids: [42], scheduled_in: fresh }), // mine, fresh
          flight({ id: 2, passenger_ids: [42], actual_in: '2024-01-01T10:00:00Z' }), // mine, old
          flight({ id: 3, passenger_ids: [1], scheduled_in: fresh }), // not mine, fresh
        ],
      },
      false,
    );
    const { result } = renderHook(() => useVisibleFlights());
    expect(result.current.map((f) => f.id)).toEqual([1]);
  });

  it('drops a flight that ages past 24h on the next tick', () => {
    // Flight will cross the 24h threshold 30 minutes into the future.
    const arrIso = new Date(Date.now() - 23.5 * 60 * 60 * 1000).toISOString();
    useStore.setState(
      {
        flights: [flight({ id: 1, actual_in: arrIso })],
        showOld: false,
      },
      false,
    );
    const { result } = renderHook(() => useVisibleFlights());
    expect(result.current).toHaveLength(1);

    // Advance system clock by 31 minutes (now ~24.0h since arrival), then
    // fire the OLD_TICK_MS interval that the hook installed.
    act(() => {
      vi.setSystemTime(new Date(Date.now() + 31 * 60 * 1000));
      vi.advanceTimersByTime(OLD_TICK_MS);
    });
    expect(result.current).toHaveLength(0);
  });
});

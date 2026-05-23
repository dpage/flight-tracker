import { useEffect, useMemo, useState } from 'react';

import type { Flight } from '../api/types';
import { useStore } from './store';

const OLD_THRESHOLD_MS = 24 * 60 * 60 * 1000;

/** How often `useVisibleFlights` re-evaluates the age filter so flights age
 * out without needing a server event. Exported for tests. */
export const OLD_TICK_MS = 60 * 1000;

/** Pure: returns true when the flight's effective arrival (actual_in,
 * else estimated_in, else scheduled_in) is more than 24h before `nowMs`.
 * Invalid timestamps fall through as not-old — matches the server's
 * COALESCE >= NOW() - 24h predicate at the boundary. */
export function isOld(f: Flight, nowMs: number): boolean {
  const arrIso = f.actual_in ?? f.estimated_in ?? f.scheduled_in;
  const arrMs = Date.parse(arrIso);
  if (Number.isNaN(arrMs)) return false;
  return arrMs < nowMs - OLD_THRESHOLD_MS;
}

/** Returns the subset of loaded flights that should be visible right now,
 * honouring the user's `showOld` and `showMineOnly` toggles and ageing
 * flights out as time passes (refreshed every OLD_TICK_MS). */
export function useVisibleFlights(): Flight[] {
  const flights = useStore((s) => s.flights);
  const showOld = useStore((s) => s.showOld);
  const showMineOnly = useStore((s) => s.showMineOnly);
  const meId = useStore((s) => s.me?.id);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (showOld) return;
    const id = window.setInterval(() => setNow(Date.now()), OLD_TICK_MS);
    return () => window.clearInterval(id);
  }, [showOld]);

  return useMemo(() => {
    let out = flights;
    if (!showOld) out = out.filter((f) => !isOld(f, now));
    // Skip the mine-only filter while "me" is unknown (auth still loading,
    // or tests that don't populate it) — otherwise we'd hide every flight
    // on first paint. "Mine" = the user is a passenger OR the creator.
    if (showMineOnly && meId != null) {
      out = out.filter((f) => f.created_by === meId || f.passenger_ids.includes(meId));
    }
    return out;
  }, [flights, showOld, showMineOnly, meId, now]);
}

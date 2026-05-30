import type { StateCreator } from 'zustand';

import { api } from '../api/client';
import type { TrackerPart } from '../api/types';
import type { StoreState } from './store';

/** Default convergence window when the user hasn't set one. */
const DEFAULT_WINDOW_BEFORE = '7d';
const DEFAULT_WINDOW_AFTER = '7d';

/** localStorage key for the tracker window, keyed per-tag, persisted the same
 * best-effort way as the `showAll` flag in coreSlice (spec §7). The empty tag
 * ('') is the untagged "everyone" view. */
function windowKey(tag: string): string {
  return `tracker.window.${tag || '_all'}`;
}

interface TrackerWindow {
  before: string;
  after: string;
}

function loadWindow(tag: string): TrackerWindow {
  try {
    const raw = window.localStorage.getItem(windowKey(tag));
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<TrackerWindow>;
      if (typeof parsed.before === 'string' && typeof parsed.after === 'string') {
        return { before: parsed.before, after: parsed.after };
      }
    }
  } catch {
    // SSR / privacy modes / malformed JSON — fall through to defaults.
  }
  return { before: DEFAULT_WINDOW_BEFORE, after: DEFAULT_WINDOW_AFTER };
}

function persistWindow(tag: string, w: TrackerWindow): void {
  try {
    window.localStorage.setItem(windowKey(tag), JSON.stringify(w));
  } catch {
    // ignore — best effort
  }
}

/** State + actions for the tracker convergence view (spec §7).
 *
 * Wave 0b: a typed fetch into `trackerParts` plus the per-tag window flag
 * persisted to localStorage. Wave 1C/2 fleshes out single-part focus and the
 * map rendering. */
export interface TrackerSlice {
  trackerParts: TrackerPart[];
  trackerTag: string;
  trackerWindow: TrackerWindow;
  trackerLoading: boolean;

  loadTracker: (opts?: { tag?: string }) => Promise<void>;
  setTrackerWindow: (w: Partial<TrackerWindow>) => Promise<void>;
  /** Apply a plan_part.updated SSE event (the poller broadcasts a
   * TrackerPartDTO). Refreshes the convergence list in place and folds the
   * live status/position into the open trip's timeline so the shared timeline
   * updates without a reload (PRD §6). Idempotent: a part the viewer can't see
   * (absent from the current list and not in the open trip) is a no-op. */
  applyPlanPartUpdate: (part: TrackerPart) => void;
}

export const createTrackerSlice: StateCreator<StoreState, [], [], TrackerSlice> = (set, get) => ({
  trackerParts: [],
  trackerTag: '',
  trackerWindow: loadWindow(''),
  trackerLoading: false,

  async loadTracker(opts) {
    const tag = opts?.tag ?? get().trackerTag;
    const w = loadWindow(tag);
    set({ trackerTag: tag, trackerWindow: w, trackerLoading: true });
    try {
      const trackerParts = await api.getTracker({
        windowBefore: w.before,
        windowAfter: w.after,
        tag: tag || undefined,
      });
      set({ trackerParts, trackerLoading: false });
    } catch (err) {
      set({ error: errorMessage(err), trackerLoading: false });
    }
  },

  async setTrackerWindow(patch) {
    const tag = get().trackerTag;
    const next = { ...get().trackerWindow, ...patch };
    persistWindow(tag, next);
    set({ trackerWindow: next });
    await get().loadTracker({ tag });
  },

  applyPlanPartUpdate(part) {
    set((s) => {
      // 1. Tracker convergence list: replace the matching row in place. Don't
      //    insert a part that isn't already listed — the list is window- and
      //    visibility-scoped server-side, and an out-of-window part shouldn't
      //    suddenly appear from a live event.
      const trackerIdx = s.trackerParts.findIndex((p) => p.plan_part_id === part.plan_part_id);
      const trackerParts =
        trackerIdx === -1
          ? s.trackerParts
          : s.trackerParts.map((p, i) => (i === trackerIdx ? part : p));

      // 2. Open trip timeline: if the changed part belongs to the trip currently
      //    on screen, fold the live fields (status / position / effective_at)
      //    into the matching PlanPart so the timeline reflects the update
      //    without a refetch. The TrackerPartDTO is a thin projection, so we
      //    merge only the fields it carries and leave the rest of the part as-is.
      let currentTrip = s.currentTrip;
      if (currentTrip && currentTrip.id === part.trip_id) {
        let touched = false;
        const plans = currentTrip.plans.map((plan) => {
          if (plan.id !== part.plan_id) return plan;
          const parts = plan.parts.map((pp) => {
            if (pp.id !== part.plan_part_id) return pp;
            touched = true;
            return {
              ...pp,
              status: (part.status || pp.status) as typeof pp.status,
              effective_at: part.effective_at || pp.effective_at,
              flight: pp.flight
                ? {
                    ...pp.flight,
                    flight_status: part.status || pp.flight.flight_status,
                    latest_position: part.latest_position ?? pp.flight.latest_position,
                  }
                : pp.flight,
            };
          });
          return touched ? { ...plan, parts } : plan;
        });
        if (touched) currentTrip = { ...currentTrip, plans };
      }

      return { trackerParts, currentTrip };
    });
  },
});

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

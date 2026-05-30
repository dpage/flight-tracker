import type { StateCreator } from 'zustand';

import { api } from '../api/client';
import type { ConfirmPlanInput, IngestInput, ProposedPlan } from '../api/types';
import type { StoreState } from './store';

/** State + actions for the paste/upload/email ingest flow (spec §6).
 *
 * Wave 0b: holds the proposed-plan list between the ingest call and the
 * confirm step. Wave 2C builds the AddToTripDialog confirm UI on top of this. */
export interface IngestSlice {
  /** Proposals returned by the most recent ingest call, awaiting confirmation. */
  ingestProposals: ProposedPlan[];
  ingestTripId: number | null;
  ingestBusy: boolean;

  /** Run an ingest (paste/upload/email) and stash the proposals. */
  ingest: (tripId: number, input: IngestInput) => Promise<void>;
  /** Commit confirmed/edited proposals, then reload the trip. */
  confirmIngest: (tripId: number, plans: ConfirmPlanInput[]) => Promise<void>;
  /** Discard pending proposals (cancel the confirm step). */
  clearIngest: () => void;
}

export const createIngestSlice: StateCreator<StoreState, [], [], IngestSlice> = (set, get) => ({
  ingestProposals: [],
  ingestTripId: null,
  ingestBusy: false,

  async ingest(tripId, input) {
    set({ ingestBusy: true });
    try {
      const res = await api.ingest(tripId, input);
      set({ ingestProposals: res.proposals, ingestTripId: tripId, ingestBusy: false });
    } catch (err) {
      // Surface to the global snackbar AND rethrow so the dialog can stay on
      // the input step rather than dropping into an empty confirm view.
      set({ error: errorMessage(err), ingestBusy: false });
      throw err;
    }
  },

  async confirmIngest(tripId, plans) {
    set({ ingestBusy: true });
    try {
      await api.ingestConfirm(tripId, plans);
      set({ ingestProposals: [], ingestTripId: null, ingestBusy: false });
      if (get().currentTrip?.id === tripId) await get().loadTrip(tripId);
    } catch (err) {
      set({ error: errorMessage(err), ingestBusy: false });
      throw err;
    }
  },

  clearIngest() {
    set({ ingestProposals: [], ingestTripId: null });
  },
});

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

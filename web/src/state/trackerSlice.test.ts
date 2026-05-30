import { describe, it, expect, beforeEach, vi } from 'vitest';

import type { Plan, PlanPart, TrackerPart, Trip } from '../api/types';

vi.mock('../api/client', () => ({
  ApiError: class {},
  api: { getTracker: vi.fn() },
}));

import { useStore } from './store';

function trackerPart(over: Partial<TrackerPart> = {}): TrackerPart {
  return {
    plan_part_id: 10,
    plan_id: 20,
    trip_id: 30,
    title: 'BA1',
    status: 'Scheduled',
    effective_at: '2024-01-01T10:00:00Z',
    ident: 'BA1',
    dest_iata: 'JFK',
    ...over,
  };
}

function part(over: Partial<PlanPart> = {}): PlanPart {
  return {
    id: 10,
    plan_id: 20,
    type: 'flight',
    seq: 0,
    starts_at: '2024-01-01T10:00:00Z',
    start_tz: 'UTC',
    end_tz: 'UTC',
    start_label: 'LHR',
    end_label: 'JFK',
    status: 'planned',
    effective_at: '2024-01-01T10:00:00Z',
    flight: {
      ident: 'BA1',
      callsign: 'BAW1',
      scheduled_out: '2024-01-01T10:00:00Z',
      scheduled_in: '2024-01-01T18:00:00Z',
      origin_iata: 'LHR',
      dest_iata: 'JFK',
      flight_status: 'Scheduled',
    },
    ...over,
  };
}

function trip(plans: Plan[]): Trip & { plans: Plan[] } {
  return {
    id: 30,
    name: 'NYC',
    destination: 'New York',
    my_role: 'owner',
    members: [],
    tags: [],
    created_at: '',
    updated_at: '',
    plans,
  };
}

function plan(parts: PlanPart[]): Plan {
  return {
    id: 20,
    trip_id: 30,
    type: 'flight',
    title: 'Outbound',
    confirmation_ref: '',
    notes: '',
    source: '',
    passenger_ids: [],
    visibility: { mode: 'everyone', user_ids: [] },
    parts,
    created_at: '',
    updated_at: '',
  };
}

beforeEach(() => {
  useStore.setState({ trackerParts: [], currentTrip: null }, false);
});

describe('applyPlanPartUpdate', () => {
  it('replaces a matching row in the tracker convergence list', () => {
    useStore.setState({ trackerParts: [trackerPart({ status: 'Scheduled' })] });
    useStore.getState().applyPlanPartUpdate(trackerPart({ status: 'Enroute' }));
    expect(useStore.getState().trackerParts[0].status).toBe('Enroute');
  });

  it('does not insert a part absent from the list (window/visibility-scoped)', () => {
    useStore.setState({ trackerParts: [trackerPart({ plan_part_id: 1 })] });
    useStore.getState().applyPlanPartUpdate(trackerPart({ plan_part_id: 999 }));
    expect(useStore.getState().trackerParts.map((p) => p.plan_part_id)).toEqual([1]);
  });

  it('folds live status/position into the matching part of the open trip', () => {
    useStore.setState({ currentTrip: trip([plan([part()])]) });
    const pos = {
      ts: '2024-01-01T12:00:00Z',
      lat: 51,
      lon: -1,
      is_estimated: false,
    };
    useStore.getState().applyPlanPartUpdate(
      trackerPart({ status: 'Enroute', latest_position: pos }),
    );
    const updated = useStore.getState().currentTrip!.plans[0].parts[0];
    expect(updated.status).toBe('Enroute');
    expect(updated.flight?.flight_status).toBe('Enroute');
    expect(updated.flight?.latest_position).toEqual(pos);
  });

  it('ignores an event for a trip other than the one on screen', () => {
    const open = trip([plan([part()])]);
    useStore.setState({ currentTrip: open });
    useStore.getState().applyPlanPartUpdate(trackerPart({ trip_id: 999, status: 'Enroute' }));
    // The open trip is untouched (same object identity, unchanged status).
    expect(useStore.getState().currentTrip).toBe(open);
    expect(useStore.getState().currentTrip!.plans[0].parts[0].status).toBe('planned');
  });
});

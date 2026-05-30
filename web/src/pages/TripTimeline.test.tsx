import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';

import type { Plan, PlanPart, Trip } from '../api/types';

const state = {
  currentTrip: null as (Trip & { plans: Plan[] }) | null,
};

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof state) => unknown) => sel(state),
}));

import TripTimeline from './TripTimeline';

function part(over: Partial<PlanPart> = {}): PlanPart {
  return {
    id: 1,
    plan_id: 1,
    type: 'flight',
    seq: 0,
    starts_at: '2026-10-12T09:00:00Z',
    start_tz: 'UTC',
    end_tz: 'UTC',
    start_label: 'LHR',
    end_label: 'LIS',
    status: 'planned',
    effective_at: '2026-10-12T09:00:00Z',
    ...over,
  };
}

function plan(parts: PlanPart[], over: Partial<Plan> = {}): Plan {
  return {
    id: parts[0]?.plan_id ?? 1,
    trip_id: 1,
    type: parts[0]?.type ?? 'flight',
    title: 'Outbound',
    confirmation_ref: '',
    notes: '',
    source: '',
    passenger_ids: [],
    visibility: { mode: 'everyone', user_ids: [] },
    parts,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

function tripWith(plans: Plan[]): Trip & { plans: Plan[] } {
  return {
    id: 1,
    name: 'Lisbon',
    destination: 'Lisbon',
    my_role: 'owner',
    members: [],
    tags: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    plans,
  };
}

function renderTimeline() {
  return render(
    <MemoryRouter>
      <TripTimeline />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  state.currentTrip = null;
});

describe('TripTimeline', () => {
  it('shows a loading state when no trip is loaded', () => {
    renderTimeline();
    expect(screen.getByText(/Loading/i)).toBeInTheDocument();
  });

  it('shows an empty state when the trip has no plans', () => {
    state.currentTrip = tripWith([]);
    renderTimeline();
    expect(screen.getByText(/Nothing on this trip yet/i)).toBeInTheDocument();
  });

  it('renders day headers and a card per part', () => {
    state.currentTrip = tripWith([
      plan([part({ id: 1, plan_id: 1, effective_at: '2026-10-12T09:00:00Z' })], {
        id: 1,
        title: 'Flight out',
      }),
    ]);
    renderTimeline();
    expect(screen.getByText(/Oct.*2026/)).toBeInTheDocument();
    expect(screen.getByText('Flight out')).toBeInTheDocument();
    expect(screen.getByTestId('part-card-1')).toBeInTheDocument();
  });

  it('drops dismissed parts', () => {
    state.currentTrip = tripWith([
      plan([part({ id: 1, dismissed_at: '2026-09-01T00:00:00Z' })]),
    ]);
    renderTimeline();
    expect(screen.getByText(/Nothing on this trip yet/i)).toBeInTheDocument();
  });

  it('marks a multi-part plan with a chip and ties the parts together', () => {
    state.currentTrip = tripWith([
      plan(
        [
          part({ id: 1, plan_id: 1, effective_at: '2026-10-12T09:00:00Z' }),
          part({ id: 2, plan_id: 1, effective_at: '2026-10-18T09:00:00Z', start_label: 'LIS', end_label: 'LHR' }),
        ],
        { id: 1, title: 'Return flights' },
      ),
    ]);
    renderTimeline();
    // Both legs render and both carry the multi-part marker.
    expect(screen.getByTestId('part-card-1')).toBeInTheDocument();
    expect(screen.getByTestId('part-card-2')).toBeInTheDocument();
    expect(screen.getAllByText('multi-part')).toHaveLength(2);
  });

  it('renders a multi-night hotel as a band with a nights label', () => {
    state.currentTrip = tripWith([
      plan(
        [
          part({
            id: 9,
            plan_id: 2,
            type: 'hotel',
            starts_at: '2026-10-12T15:00:00Z',
            ends_at: '2026-10-15T10:00:00Z',
            start_label: 'Hotel Lisboa',
            end_label: '',
          }),
        ],
        { id: 2, type: 'hotel', title: 'Hotel Lisboa' },
      ),
    ]);
    renderTimeline();
    expect(screen.getByText(/3 nights/)).toBeInTheDocument();
  });

  it('greys a cancelled (superseded old) part and tags it, not the replacement', () => {
    // On a rebooking the OLD part is stamped status='cancelled'; the NEW part
    // carries supersedes_id and stays full-colour. The greying/tag keys on the
    // cancelled status, so the OLD leg (id 3) is greyed+tagged and the NEW leg
    // (id 4, supersedes_id set, planned) is not.
    state.currentTrip = tripWith([
      plan(
        [
          part({ id: 3, status: 'cancelled', effective_at: '2026-10-12T09:00:00Z' }),
          part({
            id: 4,
            status: 'planned',
            supersedes_id: 3,
            effective_at: '2026-10-12T14:00:00Z',
          }),
        ],
        { id: 1 },
      ),
    ]);
    renderTimeline();
    expect(screen.getByText('cancelled')).toBeInTheDocument();
    // Only the cancelled part is greyed.
    const oldCard = screen.getByTestId('part-card-3');
    const newCard = screen.getByTestId('part-card-4');
    expect(oldCard).toHaveStyle({ opacity: '0.55' });
    expect(newCard).toHaveStyle({ opacity: '1' });
  });

  it('links a flight part through to the tracker', () => {
    state.currentTrip = tripWith([
      plan(
        [
          part({
            id: 4,
            type: 'flight',
            flight: {
              ident: 'TP123',
              callsign: '',
              scheduled_out: '2026-10-12T09:00:00Z',
              scheduled_in: '2026-10-12T11:00:00Z',
              origin_iata: 'LHR',
              dest_iata: 'LIS',
              flight_status: 'Scheduled',
            },
          }),
        ],
        { id: 1, title: 'Flight out' },
      ),
    ]);
    renderTimeline();
    const link = screen.getByRole('link', { name: /Track TP123/i });
    expect(link).toHaveAttribute('href', '/tracker?part=4');
  });

  it('opens the whole-plan detail dialog when a card is tapped', async () => {
    state.currentTrip = tripWith([
      plan(
        [
          part({ id: 1, plan_id: 1, effective_at: '2026-10-12T09:00:00Z', start_label: 'LHR', end_label: 'LIS' }),
          part({ id: 2, plan_id: 1, effective_at: '2026-10-18T09:00:00Z', start_label: 'LIS', end_label: 'LHR' }),
        ],
        { id: 1, title: 'Return flights', confirmation_ref: 'ABC123' },
      ),
    ]);
    renderTimeline();
    await userEvent.click(screen.getByTestId('part-card-1'));
    const dialog = screen.getByRole('dialog');
    // The dialog lists the whole booking — both legs and the confirmation ref.
    expect(dialog).toHaveTextContent('ABC123');
    expect(dialog).toHaveTextContent('LHR');
    expect(dialog).toHaveTextContent('LIS');
  });
});

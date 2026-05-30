import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Capabilities, PlanPart, ProposedPlan, Trip } from '../api/types';

// Drive the DateTimePicker through a plain controlled input so the manual
// form's dates are deterministic (same shim the FlightDialog test uses).
vi.mock('@mui/x-date-pickers/DateTimePicker', () => ({
  DateTimePicker: ({
    label,
    value,
    onChange,
  }: {
    label: string;
    value: Date | null;
    onChange: (d: Date | null) => void;
  }) => (
    <input
      aria-label={label}
      type="datetime-local"
      value={value ? new Date(value).toISOString().slice(0, 16) : ''}
      onChange={(e) => onChange(e.target.value ? new Date(e.target.value) : null)}
    />
  ),
}));

const h = vi.hoisted(() => ({
  state: {
    trips: [] as Trip[],
    currentTrip: null as (Trip & { plans: [] }) | null,
    capabilities: { resolver_available: false } as Capabilities,
    ingestProposals: [] as ProposedPlan[],
    ingestBusy: false,
    createPlan: vi.fn(),
    ingest: vi.fn(),
    confirmIngest: vi.fn(),
    clearIngest: vi.fn(),
    setError: vi.fn(),
  },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof h.state) => unknown) => sel(h.state),
}));

import AddToTripDialog from './AddToTripDialog';

function trip(over: Partial<Trip> = {}): Trip {
  return {
    id: 1,
    name: 'Lisbon',
    destination: 'Lisbon',
    my_role: 'owner',
    members: [],
    tags: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

function part(over: Partial<PlanPart> = {}): PlanPart {
  return {
    id: 1,
    plan_id: 0,
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

function proposal(over: Partial<ProposedPlan> = {}): ProposedPlan {
  return {
    type: 'flight',
    title: 'BA286',
    confirmation_ref: 'ABC123',
    notes: '',
    confidence: 0.95,
    parts: [part()],
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  h.state.trips = [];
  h.state.currentTrip = null;
  h.state.capabilities = { resolver_available: false } as Capabilities;
  h.state.ingestProposals = [];
  h.state.ingestBusy = false;
});

describe('AddToTripDialog - shell', () => {
  it('renders the four capture tabs', () => {
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    expect(screen.getByRole('tab', { name: 'Manual' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Paste text' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Upload' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'From email' })).toBeInTheDocument();
  });

  it('cancel calls onClose and clears pending proposals', async () => {
    const onClose = vi.fn();
    render(<AddToTripDialog open tripId={1} onClose={onClose} />);
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onClose).toHaveBeenCalled();
    expect(h.state.clearIngest).toHaveBeenCalled();
  });

  it('shows a trip picker and gates capture when no trip is provided', async () => {
    h.state.trips = [trip({ id: 1, name: 'Lisbon' }), trip({ id: 2, name: 'Tokyo' })];
    render(<AddToTripDialog open tripId={null} onClose={vi.fn()} />);
    expect(screen.getByText(/Pick a trip above/i)).toBeInTheDocument();
    // Submit is blocked until a trip is chosen.
    expect(screen.getByRole('button', { name: 'Add to trip' })).toBeDisabled();
  });

  it('seeds the trip from currentTrip when no tripId prop is given', () => {
    h.state.trips = [trip({ id: 5, name: 'Rome' })];
    h.state.currentTrip = { ...trip({ id: 5, name: 'Rome' }), plans: [] };
    render(<AddToTripDialog open tripId={null} onClose={vi.fn()} />);
    // No "pick a trip" gate because currentTrip seeds the selection.
    expect(screen.queryByText(/Pick a trip above/i)).not.toBeInTheDocument();
  });
});

describe('AddToTripDialog - manual tab', () => {
  it('builds a CreatePlanInput and calls createPlan', async () => {
    h.state.createPlan.mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(<AddToTripDialog open tripId={1} onClose={onClose} />);

    await userEvent.type(screen.getByLabelText(/Title/), 'Flight to Lisbon');
    await userEvent.type(screen.getByLabelText(/Flight number/), 'ba286');
    await userEvent.click(screen.getByRole('button', { name: 'Add to trip' }));

    expect(h.state.createPlan).toHaveBeenCalledTimes(1);
    const [tripId, input] = h.state.createPlan.mock.calls[0];
    expect(tripId).toBe(1);
    expect(input.type).toBe('flight');
    expect(input.title).toBe('Flight to Lisbon');
    expect(input.parts).toHaveLength(1);
    expect(input.parts[0].flight.ident).toBe('BA286');
    expect(onClose).toHaveBeenCalled();
  });

  it('disables submit until a title is entered', async () => {
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Add to trip' })).toBeDisabled();
    await userEvent.type(screen.getByLabelText(/Title/), 'Dinner');
    expect(screen.getByRole('button', { name: 'Add to trip' })).toBeEnabled();
  });

  it('surfaces createPlan errors via setError', async () => {
    h.state.createPlan.mockRejectedValue(new Error('create failed'));
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.type(screen.getByLabelText(/Title/), 'X');
    await userEvent.click(screen.getByRole('button', { name: 'Add to trip' }));
    expect(h.state.setError).toHaveBeenCalledWith('create failed');
  });
});

describe('AddToTripDialog - paste/confirm flow', () => {
  it('ingests pasted text then shows the confirm step with proposals', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [proposal({ title: 'BA286', confidence: 0.95 })];
    });
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'BA286 LHR-LIS');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));

    expect(h.state.ingest).toHaveBeenCalledWith(1, {
      text: 'BA286 LHR-LIS',
      source: 'paste',
    });
    // Confirm step takes over.
    expect(screen.getByText('Confirm extracted plans')).toBeInTheDocument();
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('BA286');
  });

  it('flags a low-confidence proposal', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [proposal({ confidence: 0.3 })];
    });
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'fuzzy');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));
    expect(screen.getByText(/Low confidence/i)).toBeInTheDocument();
  });

  it('confirms edited proposals via confirmIngest', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [proposal({ title: 'BA286' })];
    });
    h.state.confirmIngest.mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(<AddToTripDialog open tripId={1} onClose={onClose} />);

    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'BA286');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));

    const title = screen.getByLabelText('Title');
    await userEvent.clear(title);
    await userEvent.type(title, 'Edited title');
    await userEvent.click(screen.getByRole('button', { name: 'Add to trip' }));

    expect(h.state.confirmIngest).toHaveBeenCalledTimes(1);
    const [tripId, plans] = h.state.confirmIngest.mock.calls[0];
    expect(tripId).toBe(1);
    expect(plans).toHaveLength(1);
    expect(plans[0].title).toBe('Edited title');
    expect(onClose).toHaveBeenCalled();
  });

  it('offers a supersession choice and carries it through on confirm', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [proposal({ supersedes_part_id: 42 })];
    });
    h.state.confirmIngest.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'rebooking');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));

    // Default keeps the supersession (replace existing).
    expect(screen.getByText(/replaces an existing plan part|rebooking/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Add to trip' }));
    const plans = h.state.confirmIngest.mock.calls[0][1];
    expect(plans[0].supersedes_part_id).toBe(42);
  });

  it('drops the supersession when the user chooses to keep the existing part', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [proposal({ supersedes_part_id: 42 })];
    });
    h.state.confirmIngest.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'rebooking');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));

    // Switch the supersession select to "keep existing". The confirm step has
    // a single combobox (the supersession choice); open it and pick the
    // "add as a new part" option.
    await userEvent.click(screen.getByRole('combobox'));
    await userEvent.click(await screen.findByRole('option', { name: /Add as a new part/i }));
    await userEvent.click(screen.getByRole('button', { name: 'Add to trip' }));

    const plans = h.state.confirmIngest.mock.calls[0][1];
    expect(plans[0].supersedes_part_id).toBeUndefined();
  });

  it('skipping a proposal excludes it from the confirm payload', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [
        proposal({ title: 'Keep me' }),
        proposal({ title: 'Skip me', parts: [part({ id: 2 })] }),
      ];
    });
    h.state.confirmIngest.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'two plans');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));

    // Skip the second proposal.
    const second = screen.getByTestId('proposal-1');
    await userEvent.click(within(second).getByRole('button', { name: 'Skip this one' }));
    await userEvent.click(screen.getByRole('button', { name: 'Add to trip' }));

    const plans = h.state.confirmIngest.mock.calls[0][1];
    expect(plans).toHaveLength(1);
    expect(plans[0].title).toBe('Keep me');
  });

  it('shows a "nothing found" message when ingest returns no proposals', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [];
    });
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'gibberish');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));
    expect(screen.getByText(/couldn.t find any plans/i)).toBeInTheDocument();
  });

  it('stays on the input step when ingest throws (e.g. 501)', async () => {
    h.state.ingest.mockRejectedValue(new Error('not implemented'));
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'anything');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));
    // No confirm step; input remains.
    expect(screen.queryByText('Confirm extracted plans')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Extract plan' })).toBeInTheDocument();
  });
});

describe('AddToTripDialog - from email tab', () => {
  it('shows the forwarding address when email ingest is enabled', async () => {
    h.state.capabilities = {
      resolver_available: false,
      email_ingest_enabled: true,
      email_ingest_address: 'trips@aerly.test',
    } as Capabilities;
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'From email' }));
    const link = screen.getByRole('link', { name: 'trips@aerly.test' });
    expect(link).toHaveAttribute('href', 'mailto:trips@aerly.test');
  });

  it('explains when email ingest is disabled', async () => {
    h.state.capabilities = {
      resolver_available: false,
      email_ingest_enabled: false,
    } as Capabilities;
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'From email' }));
    expect(screen.getByText(/isn.t enabled on this server/i)).toBeInTheDocument();
  });
});

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Capabilities, Flight, User } from '../api/types';

// We mock @mui/x-date-pickers DatePicker/DateTimePicker with a plain controlled
// <input type="datetime-local"> that calls onChange(new Date(value)). This lets
// us drive dates deterministically through the DOM and hit every submit branch
// without driving the real (portal/calendar) picker UI.
vi.mock('@mui/x-date-pickers/DatePicker', () => ({
  DatePicker: ({
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
  api: { resolveFlight: vi.fn() },
  state: {
    users: [] as User[],
    flights: [] as Flight[],
    capabilities: { resolver_available: false } as Capabilities,
    createFlight: vi.fn(),
    updateFlight: vi.fn(),
    addPassenger: vi.fn(),
    removePassenger: vi.fn(),
    setError: vi.fn(),
  },
}));

vi.mock('../api/client', () => ({ api: h.api }));
vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof h.state) => unknown) => sel(h.state),
}));

import FlightDialog from './FlightDialog';

function user(over: Partial<User> = {}): User {
  return {
    id: 1,
    github_login: 'octocat',
    name: 'Octo',
    avatar_url: '',
    is_superuser: false,
    is_active: true,
    has_logged_in: true,
    ...over,
  };
}

function flight(over: Partial<Flight> = {}): Flight {
  return {
    id: 1,
    ident: 'BA1',
    icao24: 'abc123',
    scheduled_out: '2024-01-01T10:00:00.000Z',
    scheduled_in: '2024-01-01T12:00:00.000Z',
    origin_iata: 'LHR',
    dest_iata: 'JFK',
    status: 'Scheduled',
    notes: 'orig notes',
    passenger_ids: [],
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  h.state.users = [];
  h.state.flights = [];
  h.state.capabilities = { resolver_available: false };
});

describe('FlightDialog - full form (create)', () => {
  it('renders the full form when resolver unavailable and creates a flight', async () => {
    h.state.capabilities = { resolver_available: false };
    h.state.users = [user({ id: 9, github_login: 'amy' })];
    h.state.createFlight.mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(<FlightDialog open editId={null} onClose={onClose} />);

    await userEvent.type(screen.getByLabelText(/^Flight number/), 'ba999');
    await userEvent.type(screen.getByLabelText('Origin IATA'), 'lhr');
    await userEvent.type(screen.getByLabelText('Destination IATA'), 'jfk');
    await userEvent.type(screen.getByLabelText('ICAO24 (optional)'), 'AABB11');
    // emptyForm() pre-fills the date pickers so canSubmitFull is already true.
    await userEvent.click(screen.getByRole('button', { name: 'Create' }));

    expect(h.state.createFlight).toHaveBeenCalledTimes(1);
    const input = h.state.createFlight.mock.calls[0][0];
    expect(input.ident).toBe('BA999');
    expect(input.origin_iata).toBe('LHR');
    expect(input.dest_iata).toBe('JFK');
    expect(input.icao24).toBe('aabb11');
    expect(onClose).toHaveBeenCalled();
  });

  it('omits icao24 when blank (|| undefined branch)', async () => {
    h.state.createFlight.mockResolvedValue(undefined);
    render(<FlightDialog open editId={null} onClose={vi.fn()} />);
    await userEvent.type(screen.getByLabelText(/^Flight number/), 'x1');
    await userEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(h.state.createFlight.mock.calls[0][0].icao24).toBeUndefined();
  });

  it('disables submit when invalid (arrival not after departure)', async () => {
    render(<FlightDialog open editId={null} onClose={vi.fn()} />);
    const dep = screen.getByLabelText('Scheduled departure (UTC)');
    const arr = screen.getByLabelText('Scheduled arrival (UTC)');
    await userEvent.type(screen.getByLabelText(/^Flight number/), 'x1');
    // Make arrival before departure.
    await userEvent.clear(arr);
    fireChange(dep, '2024-05-01T10:00');
    fireChange(arr, '2024-05-01T09:00');
    expect(screen.getByRole('button', { name: 'Create' })).toBeDisabled();
  });

  it('clearing departure date disables submit (scheduledOut null)', async () => {
    render(<FlightDialog open editId={null} onClose={vi.fn()} />);
    await userEvent.type(screen.getByLabelText(/^Flight number/), 'x1');
    fireChange(screen.getByLabelText('Scheduled departure (UTC)'), '');
    expect(screen.getByRole('button', { name: 'Create' })).toBeDisabled();
  });

  it('surfaces createFlight errors via setError', async () => {
    h.state.createFlight.mockRejectedValue(new Error('create failed'));
    render(<FlightDialog open editId={null} onClose={vi.fn()} />);
    await userEvent.type(screen.getByLabelText(/^Flight number/), 'x1');
    await userEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(h.state.setError).toHaveBeenCalledWith('create failed');
  });

  it('cancel button calls onClose', async () => {
    const onClose = vi.fn();
    render(<FlightDialog open editId={null} onClose={onClose} />);
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onClose).toHaveBeenCalled();
  });
});

describe('FlightDialog - minimal form', () => {
  beforeEach(() => {
    h.state.capabilities = { resolver_available: true };
  });

  it('uses the minimal form and resolves + creates on success', async () => {
    h.api.resolveFlight.mockResolvedValue({
      ident: 'BA286',
      scheduled_out: '2024-02-01T10:00:00Z',
      scheduled_in: '2024-02-01T20:00:00Z',
      origin_iata: 'SFO',
      origin_lat: 1,
      origin_lon: 2,
      dest_iata: 'LHR',
      dest_lat: 3,
      dest_lon: 4,
      icao24: '',
      notes: 'resolver notes',
    });
    h.state.createFlight.mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(<FlightDialog open editId={null} onClose={onClose} />);

    await userEvent.type(screen.getByLabelText(/^Flight number/), 'ba286');
    await userEvent.click(screen.getByRole('button', { name: 'Look up & add' }));

    expect(h.api.resolveFlight).toHaveBeenCalled();
    const arg = h.api.resolveFlight.mock.calls[0][0];
    expect(arg.ident).toBe('BA286');
    expect(arg.date).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(h.state.createFlight).toHaveBeenCalled();
    const created = h.state.createFlight.mock.calls[0][0];
    expect(created.icao24).toBeUndefined(); // resolved.icao24 '' -> undefined
    expect(created.notes).toBe('resolver notes'); // blank notes -> resolver default
    expect(onClose).toHaveBeenCalled();
  });

  it('uses typed notes over resolver notes', async () => {
    h.api.resolveFlight.mockResolvedValue({
      ident: 'BA1',
      scheduled_out: 'a',
      scheduled_in: 'b',
      origin_iata: 'A',
      origin_lat: 0,
      origin_lon: 0,
      dest_iata: 'B',
      dest_lat: 0,
      dest_lon: 0,
      icao24: 'deadbe',
      notes: 'resolver',
    });
    h.state.createFlight.mockResolvedValue(undefined);
    render(<FlightDialog open editId={null} onClose={vi.fn()} />);
    await userEvent.type(screen.getByLabelText(/^Flight number/), 'ba1');
    await userEvent.type(screen.getByLabelText('Notes (optional)'), 'mine');
    await userEvent.click(screen.getByRole('button', { name: 'Look up & add' }));
    const created = h.state.createFlight.mock.calls[0][0];
    expect(created.notes).toBe('mine');
    expect(created.icao24).toBe('deadbe');
  });

  it('resolver failure drops into prefilled full form + manual override', async () => {
    h.api.resolveFlight.mockRejectedValue(new Error('not found'));
    render(<FlightDialog open editId={null} onClose={vi.fn()} />);
    await userEvent.type(screen.getByLabelText(/^Flight number/), 'zz9');
    await userEvent.type(screen.getByLabelText('Notes (optional)'), 'note');
    await userEvent.click(screen.getByRole('button', { name: 'Look up & add' }));
    expect(h.state.setError).toHaveBeenCalledWith('not found');
    // Now showing the full form (manualOverride true) prefilled.
    expect(await screen.findByText(/Entering everything manually/i)).toBeInTheDocument();
    expect((screen.getByLabelText(/^Flight number/) as HTMLInputElement).value).toBe('ZZ9');
  });

  it('switches to manual entry and back via the links', async () => {
    render(<FlightDialog open editId={null} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('button', { name: /manual entry/i }));
    expect(screen.getByText(/Entering everything manually/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /look it up instead/i }));
    expect(screen.getByLabelText('Departure date (UTC)')).toBeInTheDocument();
  });

  it('minimal submit disabled with blank ident; clearing date disables too', async () => {
    render(<FlightDialog open editId={null} onClose={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Look up & add' })).toBeDisabled();
    await userEvent.type(screen.getByLabelText(/^Flight number/), 'a1');
    expect(screen.getByRole('button', { name: 'Look up & add' })).toBeEnabled();
    fireChange(screen.getByLabelText('Departure date (UTC)'), '');
    expect(screen.getByRole('button', { name: 'Look up & add' })).toBeDisabled();
  });

  it('renders selected passenger chips in the minimal form (renderTags/getOptionLabel)', async () => {
    h.state.users = [user({ id: 1, github_login: 'amy' }), user({ id: 2, github_login: 'bob' })];
    render(<FlightDialog open editId={null} onClose={vi.fn()} />);
    const auto = screen.getByLabelText('Passengers');
    await userEvent.click(auto);
    await userEvent.click(await screen.findByText('amy'));
    // Chip rendered by the minimal-form renderTags.
    expect(screen.getByText('amy').closest('.MuiChip-root')).toBeTruthy();
    // Avatar fallback initial inside the chip.
    expect(screen.getByText('A')).toBeInTheDocument();
  });

  it('resolver failure with non-Error sets String() and null date arrival', async () => {
    h.api.resolveFlight.mockRejectedValue('strerr');
    render(<FlightDialog open editId={null} onClose={vi.fn()} />);
    await userEvent.type(screen.getByLabelText(/^Flight number/), 'q1');
    await userEvent.click(screen.getByRole('button', { name: 'Look up & add' }));
    expect(h.state.setError).toHaveBeenCalledWith('strerr');
  });
});

describe('FlightDialog - editing', () => {
  it('pre-populates the form from the edited flight and builds a partial patch', async () => {
    const f = flight({
      id: 7,
      ident: 'AA7',
      icao24: 'oldica',
      notes: 'orig notes',
      status: 'Scheduled',
      passenger_ids: [1],
    });
    h.state.flights = [f];
    h.state.users = [user({ id: 1, github_login: 'amy' }), user({ id: 2, github_login: 'bob' })];
    h.state.updateFlight.mockResolvedValue(undefined);
    h.state.addPassenger.mockResolvedValue(undefined);
    h.state.removePassenger.mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(<FlightDialog open editId={7} onClose={onClose} />);

    expect(screen.getByText('Edit AA7')).toBeInTheDocument();
    expect((screen.getByLabelText(/^Flight number/) as HTMLInputElement).value).toBe('AA7');

    // Change several fields so the patch includes them.
    const notes = screen.getByLabelText('Notes');
    await userEvent.clear(notes);
    await userEvent.type(notes, 'changed notes');
    const origin = screen.getByLabelText('Origin IATA');
    await userEvent.clear(origin);
    await userEvent.type(origin, 'cdg');
    const icao = screen.getByLabelText('ICAO24 (optional)');
    await userEvent.clear(icao);
    await userEvent.type(icao, 'newica');
    // Change status
    await userEvent.selectOptions(screen.getByLabelText('Status'), 'Boarding');
    // Change times
    fireChange(screen.getByLabelText('Scheduled departure (UTC)'), '2024-03-01T08:00');
    fireChange(screen.getByLabelText('Scheduled arrival (UTC)'), '2024-03-01T12:00');

    await userEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(h.state.updateFlight).toHaveBeenCalledWith(
      7,
      expect.objectContaining({
        notes: 'changed notes',
        origin_iata: 'CDG',
        icao24: 'newica',
        status: 'Boarding',
      }),
    );
    const patch = h.state.updateFlight.mock.calls[0][1];
    expect(patch.scheduled_out).toBeDefined();
    expect(patch.scheduled_in).toBeDefined();
    // passenger 1 existed and remains (no add/remove since unchanged)
    expect(h.state.removePassenger).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it('adds and removes passengers on edit', async () => {
    const f = flight({ id: 8, passenger_ids: [1] });
    h.state.flights = [f];
    h.state.users = [user({ id: 1, github_login: 'amy' }), user({ id: 2, github_login: 'bob' })];
    h.state.updateFlight.mockResolvedValue(undefined);
    h.state.addPassenger.mockResolvedValue(undefined);
    h.state.removePassenger.mockResolvedValue(undefined);
    render(<FlightDialog open editId={8} onClose={vi.fn()} />);

    // Remove amy (id 1), add bob (id 2) via the Autocomplete.
    const auto = screen.getByLabelText('Passengers');
    await userEvent.click(auto);
    await userEvent.click(await screen.findByText('bob'));
    // Now both selected; clear amy chip.
    // Delete amy: find the chip delete (CancelIcon) within the amy chip.
    const amyChip = screen.getByText('amy').closest('.MuiChip-root') as HTMLElement;
    const del = within(amyChip).getByTestId('CancelIcon');
    await userEvent.click(del);

    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(h.state.addPassenger).toHaveBeenCalledWith(8, 2);
    expect(h.state.removePassenger).toHaveBeenCalledWith(8, 1);
  });

  it('makes no updateFlight call when nothing changed (empty patch)', async () => {
    const f = flight({ id: 9, passenger_ids: [] });
    h.state.flights = [f];
    h.state.users = [];
    render(<FlightDialog open editId={9} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(h.state.updateFlight).not.toHaveBeenCalled();
  });

  it('handles editId that does not match any flight (editing null -> add flight)', () => {
    h.state.flights = [flight({ id: 1 })];
    render(<FlightDialog open editId={555} onClose={vi.fn()} />);
    expect(screen.getByText('Add flight')).toBeInTheDocument();
  });

  it('edit submit error surfaces via setError', async () => {
    const f = flight({ id: 10, notes: 'a' });
    h.state.flights = [f];
    h.state.updateFlight.mockRejectedValue(new Error('patch failed'));
    render(<FlightDialog open editId={10} onClose={vi.fn()} />);
    const notes = screen.getByLabelText('Notes');
    await userEvent.clear(notes);
    await userEvent.type(notes, 'b');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(h.state.setError).toHaveBeenCalledWith('patch failed');
  });
});

describe('FlightDialog - closed/no-op', () => {
  it('does not reset form while closed (open=false effect early return)', () => {
    const { container } = render(<FlightDialog open={false} editId={null} onClose={vi.fn()} />);
    // Dialog is not mounted in the DOM.
    expect(container.querySelector('input')).toBeNull();
  });
});

function fireChange(el: HTMLElement, value: string) {
  const input = el as HTMLInputElement;
  const setter = Object.getOwnPropertyDescriptor(
    window.HTMLInputElement.prototype,
    'value',
  )!.set!;
  setter.call(input, value);
  input.dispatchEvent(new Event('input', { bubbles: true }));
}

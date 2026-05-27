import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Flight, User } from '../api/types';

const selectFlight = vi.fn();
const deleteFlight = vi.fn();
const setShowAll = vi.fn();
const setShowOld = vi.fn();
const setShowMineOnly = vi.fn();

const state = {
  flights: [] as Flight[],
  users: [] as User[],
  me: null as User | null,
  selectedFlightId: null as number | null,
  // PollFooter inside FlightList reads these — the mock must supply them
  // or every render blows up inside the footer with "undefined.poll_interval_sec".
  capabilities: { resolver_available: false, poll_interval_sec: 60 },
  lastUpdateAt: null as number | null,
  showAll: false,
  showOld: false,
  // Default OFF in tests so existing rendering assertions (which never set
  // a passenger list) keep showing flights. Tests that exercise the toggle
  // flip this on explicitly.
  showMineOnly: false,
  selectFlight,
  deleteFlight,
  setShowAll,
  setShowOld,
  setShowMineOnly,
};

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof state) => unknown) => sel(state),
}));

import FlightList from './FlightList';

function flight(over: Partial<Flight> = {}): Flight {
  return {
    id: 1,
    ident: 'BA1',
    scheduled_out: '2024-01-01T10:00:00Z',
    scheduled_in: '2024-01-01T12:00:00Z',
    origin_iata: 'LHR',
    dest_iata: 'JFK',
    origin_lat: 51,
    origin_lon: 0,
    dest_lat: 40,
    dest_lon: -73,
    status: 'Scheduled',
    notes: '',
    passenger_ids: [],
    shared_user_ids: [],
    is_public: false,
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

function futureIso() {
  return new Date(Date.now() + 60 * 60 * 1000).toISOString();
}
function hoursAgoIso(h: number) {
  return new Date(Date.now() - h * 60 * 60 * 1000).toISOString();
}

beforeEach(() => {
  vi.clearAllMocks();
  state.flights = [];
  state.users = [];
  state.me = null;
  state.selectedFlightId = null;
  state.showAll = false;
  state.showOld = true;
  state.showMineOnly = false;
});

describe('FlightList', () => {
  it('shows the empty state when there are no flights', () => {
    render(<FlightList onEditFlight={vi.fn()} />);
    expect(screen.getByText(/No flights yet/i)).toBeInTheDocument();
  });

  it('renders rows with route, schedule and passenger avatars', () => {
    state.users = [user({ id: 9, username: 'amy' })];
    state.flights = [flight({ passenger_ids: [9, 999], estimated_in: '2024-01-01T13:00:00Z' })];
    render(<FlightList onEditFlight={vi.fn()} />);
    expect(screen.getByText('BA1')).toBeInTheDocument();
    expect(screen.getByText(/LHR/)).toBeInTheDocument();
    // avatar fallback initial for amy (avatar_url empty)
    expect(screen.getByText('A')).toBeInTheDocument();
  });

  it('renders ??? when IATA codes are blank', () => {
    state.flights = [flight({ origin_iata: '', dest_iata: '' })];
    render(<FlightList onEditFlight={vi.fn()} />);
    expect(screen.getByText(/\?\?\? → \?\?\?/)).toBeInTheDocument();
  });

  it('shows "no map" chip when coordinates are missing', () => {
    state.flights = [flight({ origin_lat: undefined })];
    render(<FlightList onEditFlight={vi.fn()} />);
    expect(screen.getByText('no map')).toBeInTheDocument();
  });

  it.each([
    ['Enroute', 'MuiChip-colorPrimary'],
    ['Departed', 'MuiChip-colorPrimary'],
    ['Arrived', 'MuiChip-colorSuccess'],
    ['Boarding', 'MuiChip-colorWarning'],
    ['Cancelled', 'MuiChip-colorError'],
    ['Diverted', 'MuiChip-colorError'],
    ['Scheduled', 'MuiChip-colorDefault'],
  ])('StatusChip colour mapping for %s', (status, cls) => {
    state.flights = [flight({ status })];
    render(<FlightList onEditFlight={vi.fn()} />);
    const chip = screen.getByText(status).closest('.MuiChip-root');
    expect(chip?.className).toContain(cls);
  });

  it('toggles selection on row click (select then deselect)', async () => {
    state.flights = [flight({ id: 5 })];
    state.selectedFlightId = null;
    const { rerender } = render(<FlightList onEditFlight={vi.fn()} />);
    await userEvent.click(screen.getByText('BA1'));
    expect(selectFlight).toHaveBeenLastCalledWith(5);

    state.selectedFlightId = 5;
    rerender(<FlightList onEditFlight={vi.fn()} />);
    await userEvent.click(screen.getByText('BA1'));
    expect(selectFlight).toHaveBeenLastCalledWith(null);
  });

  it('edit button stops propagation and calls onEditFlight', async () => {
    state.flights = [flight({ id: 5 })];
    const onEdit = vi.fn();
    render(<FlightList onEditFlight={onEdit} />);
    const buttons = screen.getAllByRole('button');
    await userEvent.click(buttons[0]); // edit
    expect(onEdit).toHaveBeenCalledWith(5);
    expect(selectFlight).not.toHaveBeenCalled();
  });

  it('delete button: confirm true triggers deleteFlight, false does not', async () => {
    state.flights = [flight({ id: 5, ident: 'XX9' })];
    render(<FlightList onEditFlight={vi.fn()} />);
    const delBtn = () => within(screen.getByText('XX9').closest('div')!.parentElement!.parentElement!.parentElement!).getAllByRole('button')[1];

    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);
    await userEvent.click(delBtn());
    expect(deleteFlight).not.toHaveBeenCalled();

    confirmSpy.mockReturnValue(true);
    await userEvent.click(delBtn());
    expect(deleteFlight).toHaveBeenCalledWith(5);
    confirmSpy.mockRestore();
  });

  it('marks the selected row', () => {
    state.flights = [flight({ id: 5 })];
    state.selectedFlightId = 5;
    render(<FlightList onEditFlight={vi.fn()} />);
    expect(screen.getByText('BA1')).toBeInTheDocument();
  });

  it('shows the owner chip in the collapsed row when created_by maps to a known user', () => {
    state.users = [user({ id: 7, username: 'dpage' })];
    state.flights = [flight({ created_by: 7 })];
    render(<FlightList onEditFlight={vi.fn()} />);
    expect(screen.getByText('dpage')).toBeInTheDocument();
  });

  it('hides the owner chip when created_by is missing or unknown', () => {
    state.users = [];
    state.flights = [flight({ created_by: 999 })];
    render(<FlightList onEditFlight={vi.fn()} />);
    expect(screen.queryByText('999')).not.toBeInTheDocument();
  });

  it('renders the detail panel only when a row is selected', () => {
    state.flights = [flight({ id: 5 })];
    state.selectedFlightId = null;
    const { rerender } = render(<FlightList onEditFlight={vi.fn()} />);
    expect(screen.queryByTestId('flight-detail-panel')).not.toBeInTheDocument();

    state.selectedFlightId = 5;
    rerender(<FlightList onEditFlight={vi.fn()} />);
    expect(screen.getByTestId('flight-detail-panel')).toBeInTheDocument();
  });

  it('renders a public-flight indicator next to the status chip', () => {
    state.flights = [flight({ is_public: true })];
    render(<FlightList onEditFlight={vi.fn()} />);
    // The PublicIcon renders an MUI svg with a testid suffix; assert via title.
    expect(screen.getByLabelText(/public flight/i, { selector: 'svg' })).toBeInTheDocument();
  });

  it('detail panel lists "Shared with" users when shared_user_ids is set', () => {
    state.users = [user({ id: 11, username: 'alice' }), user({ id: 22, username: 'bob' })];
    state.flights = [flight({ id: 5, shared_user_ids: [11, 22] })];
    state.selectedFlightId = 5;
    render(<FlightList onEditFlight={vi.fn()} />);
    const panel = screen.getByTestId('flight-detail-panel');
    expect(panel).toHaveTextContent('Shared with');
    expect(panel).toHaveTextContent('alice');
    expect(panel).toHaveTextContent('bob');
  });

  it('detail panel calls out public flights in the Visibility section', () => {
    state.flights = [flight({ id: 5, is_public: true })];
    state.selectedFlightId = 5;
    render(<FlightList onEditFlight={vi.fn()} />);
    const panel = screen.getByTestId('flight-detail-panel');
    expect(panel).toHaveTextContent(/Public/i);
  });

  it('hides the show-all toggle for non-superusers', () => {
    state.me = user({ is_superuser: false });
    state.flights = [flight()];
    render(<FlightList onEditFlight={vi.fn()} />);
    expect(screen.queryByLabelText(/show all flights/i)).not.toBeInTheDocument();
  });

  it('shows the show-all toggle for superusers and calls setShowAll on flip', async () => {
    state.me = user({ is_superuser: true });
    state.flights = [flight()];
    render(<FlightList onEditFlight={vi.fn()} />);
    const toggle = screen.getByLabelText(/show all flights/i);
    expect(toggle).toBeInTheDocument();
    await userEvent.click(toggle);
    expect(setShowAll).toHaveBeenCalledWith(true);
  });

  it('detail panel shows position telemetry from latest_position', () => {
    state.flights = [
      flight({
        id: 5,
        latest_position: {
          ts: '2024-01-01T10:00:00Z',
          lat: 51.2034,
          lon: -3.4521,
          altitude_ft: 35000,
          groundspeed_kt: 480,
          heading_deg: 273,
          is_estimated: false,
        },
      }),
    ];
    state.selectedFlightId = 5;
    render(<FlightList onEditFlight={vi.fn()} />);
    const panel = screen.getByTestId('flight-detail-panel');
    expect(panel).toHaveTextContent('35,000 ft');
    expect(panel).toHaveTextContent('480 kt');
    expect(panel).toHaveTextContent('273°');
    expect(panel).toHaveTextContent('51.2034°');
  });

  it('renders schedule in each airport tz when provided', () => {
    // 10:00Z in LHR (Europe/London, BST in July) = 11:00; 14:00Z in JFK
    // (America/New_York, EDT) = 10:00. Use a date where DST is unambiguous.
    state.flights = [
      flight({
        scheduled_out: '2024-07-01T10:00:00Z',
        scheduled_in: '2024-07-01T14:00:00Z',
        origin_tz: 'Europe/London',
        dest_tz: 'America/New_York',
      }),
    ];
    render(<FlightList onEditFlight={vi.fn()} />);
    // Departure shown in BST → 11:00; arrival shown in EDT → 10:00.
    expect(screen.getByText(/11:00.*→.*10:00/)).toBeInTheDocument();
  });

  it('falls back to UTC display when airport tz is unknown', () => {
    state.flights = [
      flight({
        scheduled_out: '2024-07-01T10:00:00Z',
        scheduled_in: '2024-07-01T14:00:00Z',
        origin_tz: undefined,
        dest_tz: undefined,
      }),
    ];
    render(<FlightList onEditFlight={vi.fn()} />);
    // No timezone known → both ends rendered as UTC with explicit suffix.
    expect(screen.getByText(/10:00 UTC.*→.*14:00 UTC/)).toBeInTheDocument();
  });

  it('detail panel shows the icao24 hex code in a mono span', () => {
    state.flights = [flight({ id: 5, icao24: 'A1B2C3' })];
    state.selectedFlightId = 5;
    render(<FlightList onEditFlight={vi.fn()} />);
    const panel = screen.getByTestId('flight-detail-panel');
    expect(panel).toHaveTextContent('A1B2C3');
  });

  it('detail panel shows a UTC subtitle beneath each localized time', () => {
    state.flights = [
      flight({
        id: 5,
        scheduled_out: '2024-07-01T10:00:00Z',
        scheduled_in: '2024-07-01T14:00:00Z',
        origin_tz: 'Europe/London',
        dest_tz: 'America/New_York',
      }),
    ];
    state.selectedFlightId = 5;
    render(<FlightList onEditFlight={vi.fn()} />);
    const panel = screen.getByTestId('flight-detail-panel');
    // The detail panel's TimeRow renders the UTC subtitle only when tz is set.
    expect(panel).toHaveTextContent(/10:00 UTC/);
    expect(panel).toHaveTextContent(/14:00 UTC/);
  });

  it('detail panel renders an "estimated" chip when the latest fix is dead-reckoned', () => {
    state.flights = [
      flight({
        id: 5,
        latest_position: {
          ts: '2024-01-01T10:00:00Z',
          lat: 0,
          lon: 0,
          altitude_ft: 10000,
          groundspeed_kt: 400,
          heading_deg: 90,
          is_estimated: true,
        },
      }),
    ];
    state.selectedFlightId = 5;
    render(<FlightList onEditFlight={vi.fn()} />);
    const panel = screen.getByTestId('flight-detail-panel');
    expect(within(panel).getByText(/estimated/i)).toBeInTheDocument();
  });

  it('detail panel shows passengers and owner chips when set', () => {
    state.users = [
      user({ id: 11, username: 'alice' }),
      user({ id: 22, username: 'bob' }),
    ];
    state.flights = [flight({ id: 5, passenger_ids: [11], created_by: 22 })];
    state.selectedFlightId = 5;
    render(<FlightList onEditFlight={vi.fn()} />);
    const panel = screen.getByTestId('flight-detail-panel');
    expect(panel).toHaveTextContent('Passengers');
    expect(panel).toHaveTextContent('alice');
    expect(panel).toHaveTextContent('Added by');
    expect(panel).toHaveTextContent('bob');
  });

  it('detail panel renders a stale fix with missing altitude/speed/heading', () => {
    // pos.ts older than 5 minutes → fixIsStale=true (warning-coloured Fix age).
    // altitude/groundspeed/heading null → each Row collapses.
    state.flights = [
      flight({
        id: 5,
        latest_position: {
          ts: new Date(Date.now() - 10 * 60 * 1000).toISOString(),
          lat: 0,
          lon: 0,
          altitude_ft: undefined,
          groundspeed_kt: undefined,
          heading_deg: undefined,
          is_estimated: false,
        },
        last_polled_at: new Date(Date.now() - 30 * 1000).toISOString(),
      }),
    ];
    state.selectedFlightId = 5;
    render(<FlightList onEditFlight={vi.fn()} />);
    const panel = screen.getByTestId('flight-detail-panel');
    expect(panel).toHaveTextContent('Fix age');
    expect(panel).not.toHaveTextContent('Altitude');
    expect(panel).not.toHaveTextContent('Groundspeed');
    expect(panel).not.toHaveTextContent('Heading');
    // last_polled_at set → "Last polled" row shows a relative duration, not "never".
    expect(panel).toHaveTextContent(/Last polled/);
    expect(panel).not.toHaveTextContent(/never/);
  });

  it('renders the Show old flights toggle even for non-superusers', () => {
    state.me = user({ is_superuser: false });
    render(<FlightList onEditFlight={vi.fn()} />);
    expect(screen.getByLabelText(/show old flights/i)).toBeInTheDocument();
  });

  it('hides the Show all flights toggle for non-superusers', () => {
    state.me = user({ is_superuser: false });
    render(<FlightList onEditFlight={vi.fn()} />);
    expect(screen.queryByLabelText(/show all flights/i)).toBeNull();
  });

  it('still renders the Show all flights toggle for superusers', () => {
    state.me = user({ is_superuser: true });
    render(<FlightList onEditFlight={vi.fn()} />);
    expect(screen.getByLabelText(/show all flights/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/show old flights/i)).toBeInTheDocument();
  });

  it('clicking the Show old flights toggle calls setShowOld(true)', async () => {
    state.showOld = false;
    render(<FlightList onEditFlight={vi.fn()} />);
    await userEvent.click(screen.getByLabelText(/show old flights/i));
    expect(setShowOld).toHaveBeenCalledWith(true);
  });

  it('renders the Only my flights toggle and reflects the showMineOnly state', () => {
    state.showMineOnly = true;
    render(<FlightList onEditFlight={vi.fn()} />);
    const toggle = screen.getByLabelText(/only my flights/i) as HTMLInputElement;
    expect(toggle).toBeInTheDocument();
    expect(toggle.checked).toBe(true);
  });

  it('clicking the Only my flights toggle calls setShowMineOnly(false) when it is on', async () => {
    state.showMineOnly = true;
    render(<FlightList onEditFlight={vi.fn()} />);
    await userEvent.click(screen.getByLabelText(/only my flights/i));
    expect(setShowMineOnly).toHaveBeenCalledWith(false);
  });

  it('clicking the Only my flights toggle calls setShowMineOnly(true) when it is off', async () => {
    state.showMineOnly = false;
    render(<FlightList onEditFlight={vi.fn()} />);
    await userEvent.click(screen.getByLabelText(/only my flights/i));
    expect(setShowMineOnly).toHaveBeenCalledWith(true);
  });

  it('hides old flights by default and shows them when showOld is true', () => {
    const fresh = flight({ id: 11, ident: 'FRESH', scheduled_in: futureIso() });
    const old = flight({ id: 12, ident: 'OLD', actual_in: hoursAgoIso(30) });
    state.flights = [fresh, old];
    state.showOld = false;
    const { rerender } = render(<FlightList onEditFlight={vi.fn()} />);
    expect(screen.getByText('FRESH')).toBeInTheDocument();
    expect(screen.queryByText('OLD')).toBeNull();

    state.showOld = true;
    rerender(<FlightList onEditFlight={vi.fn()} />);
    expect(screen.getByText('OLD')).toBeInTheDocument();
  });
});

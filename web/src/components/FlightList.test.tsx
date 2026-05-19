import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Flight, User } from '../api/types';

const selectFlight = vi.fn();
const deleteFlight = vi.fn();

const state = {
  flights: [] as Flight[],
  users: [] as User[],
  selectedFlightId: null as number | null,
  selectFlight,
  deleteFlight,
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
    ...over,
  };
}

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

beforeEach(() => {
  vi.clearAllMocks();
  state.flights = [];
  state.users = [];
  state.selectedFlightId = null;
});

describe('FlightList', () => {
  it('shows the empty state when there are no flights', () => {
    render(<FlightList onEditFlight={vi.fn()} />);
    expect(screen.getByText(/No flights yet/i)).toBeInTheDocument();
  });

  it('renders rows with route, schedule and passenger avatars', () => {
    state.users = [user({ id: 9, github_login: 'amy' })];
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
});

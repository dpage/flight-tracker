import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';

import type { Position, TrackerPart, Trip } from '../api/types';
import maplibreMock, { resetMaplibreMock } from '../test/maplibre-mock';

vi.mock('maplibre-gl', () => ({ default: maplibreMock, ...maplibreMock }));

const loadTracker = vi.fn();
const setTrackerWindow = vi.fn().mockResolvedValue(undefined);
const listTrips = vi.fn();

const state = {
  loadTracker,
  setTrackerWindow,
  listTrips,
  trackerParts: [] as TrackerPart[],
  trackerTag: '',
  trackerWindow: { before: '7d', after: '7d' },
  trackerLoading: false,
  trips: [] as Trip[],
};

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof state) => unknown) => sel(state),
}));

import Tracker from './Tracker';

function pos(over: Partial<Position> = {}): Position {
  return { ts: '2026-01-01T10:00:00Z', lat: 50, lon: 5, is_estimated: false, ...over };
}

function part(over: Partial<TrackerPart> = {}): TrackerPart {
  return {
    plan_part_id: 1,
    plan_id: 1,
    trip_id: 1,
    title: 'BA1',
    status: 'Enroute',
    effective_at: '2026-01-01T10:00:00Z',
    ident: 'BA1',
    dest_iata: 'JFK',
    latest_position: pos(),
    ...over,
  };
}

function trip(over: Partial<Trip> = {}): Trip {
  return {
    id: 1,
    name: 'Trip',
    destination: '',
    my_role: 'owner',
    members: [],
    tags: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

function renderTracker(initial = '/tracker') {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Tracker />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  resetMaplibreMock();
  state.trackerParts = [];
  state.trackerTag = '';
  state.trackerWindow = { before: '7d', after: '7d' };
  state.trackerLoading = false;
  state.trips = [];
});

describe('Tracker page', () => {
  it('loads the tracker on mount and renders the convergence controls', async () => {
    renderTracker();
    await waitFor(() => expect(loadTracker).toHaveBeenCalled());
    expect(screen.getByLabelText('Tag')).toBeInTheDocument();
    expect(screen.getByText(/before/i)).toBeInTheDocument();
    expect(screen.getByText(/after/i)).toBeInTheDocument();
  });

  it('lists the in-window parts', () => {
    state.trackerParts = [part({ plan_part_id: 1, ident: 'BA1' }), part({ plan_part_id: 2, ident: 'LH7' })];
    renderTracker();
    expect(screen.getByText('BA1')).toBeInTheDocument();
    expect(screen.getByText('LH7')).toBeInTheDocument();
  });

  it('shows an empty-state when no parts are in the window', () => {
    renderTracker();
    expect(screen.getByText(/no travel in this window/i)).toBeInTheDocument();
  });

  it('focuses a single flight when ?part= is set: hides controls, narrows the list', () => {
    state.trackerParts = [part({ plan_part_id: 1, ident: 'BA1' }), part({ plan_part_id: 2, ident: 'LH7' })];
    renderTracker('/tracker?part=2');
    expect(screen.getByText('Single flight')).toBeInTheDocument();
    // Tag selector hidden in focus mode.
    expect(screen.queryByLabelText('Tag')).not.toBeInTheDocument();
    expect(screen.getByText('LH7')).toBeInTheDocument();
    expect(screen.queryByText('BA1')).not.toBeInTheDocument();
  });

  it('changing the tag seeds the window from the tag span and reloads', async () => {
    const user = userEvent.setup();
    const now = Date.now();
    const dayMs = 24 * 60 * 60 * 1000;
    state.trips = [
      trip({
        id: 1,
        tags: ['pgconf'],
        starts_on: new Date(now - 3 * dayMs).toISOString().slice(0, 10),
        ends_on: new Date(now + 4 * dayMs).toISOString().slice(0, 10),
      }),
    ];
    renderTracker();
    await user.click(screen.getByLabelText('Tag'));
    const listbox = await screen.findByRole('listbox');
    await user.click(within(listbox).getByText('pgconf'));
    // Window seeded from the tag's span before reloading for the tag.
    expect(setTrackerWindow).toHaveBeenCalled();
    const seeded = setTrackerWindow.mock.calls[0][0] as { before: string; after: string };
    expect(seeded.before).toMatch(/^\d+d$/);
    expect(seeded.after).toMatch(/^\d+d$/);
  });
});

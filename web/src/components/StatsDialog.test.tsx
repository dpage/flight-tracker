import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Flight } from '../api/types';

const h = vi.hoisted(() => ({
  api: {
    listFlights: vi.fn(),
  },
  setError: vi.fn(),
  me: { id: 1, github_login: 'alice', is_superuser: false } as { id: number; github_login: string; is_superuser: boolean },
}));

vi.mock('../api/client', () => ({ api: h.api }));
vi.mock('../state/store', () => ({
  useStore: (sel: (s: { me: typeof h.me; setError: typeof h.setError }) => unknown) =>
    sel({ me: h.me, setError: h.setError }),
}));

import StatsDialog from './StatsDialog';

function flight(over: Partial<Flight> = {}): Flight {
  return {
    id: 1,
    ident: 'BA286',
    scheduled_out: '2026-01-01T10:00:00Z',
    scheduled_in: '2026-01-01T14:00:00Z',
    origin_iata: 'SFO',
    origin_lat: 37.6213,
    origin_lon: -122.379,
    dest_iata: 'LHR',
    dest_lat: 51.47,
    dest_lon: -0.4543,
    status: 'Arrived',
    notes: '',
    passenger_ids: [1],
    is_public: false,
    shared_user_ids: [],
    ...over,
  } as Flight;
}

beforeEach(() => {
  vi.clearAllMocks();
  h.api.listFlights.mockResolvedValue([]);
});

describe('StatsDialog', () => {
  it('does not render when closed', () => {
    render(<StatsDialog open={false} onClose={() => {}} />);
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('fetches with showOld=true when opened and renders zeros for an empty list', async () => {
    render(<StatsDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.api.listFlights).toHaveBeenCalledWith({ showOld: true }));
    expect(screen.getByText(/includes all flights/i)).toBeInTheDocument();
    expect(screen.getByText('Flights').nextSibling?.textContent).toBe('0');
  });

  it('renders flown totals for a single arrived flight', async () => {
    h.api.listFlights.mockResolvedValue([flight()]);
    render(<StatsDialog open onClose={() => {}} />);
    // Distance tile contains miles · km; at least one element matches
    await waitFor(() => {
      expect(screen.getAllByText(/5,3\d{2} mi/).length).toBeGreaterThanOrEqual(1);
    });
    expect(screen.getByText('Time in the air').nextSibling?.textContent).toMatch(/4h/);
    expect(screen.getByText('Airports visited').nextSibling?.textContent).toBe('2');
  });

  it('switches to upcoming totals when the Upcoming tab is clicked', async () => {
    h.api.listFlights.mockResolvedValue([
      flight({ id: 1, status: 'Arrived' }),
      flight({ id: 2, status: 'Scheduled', ident: 'UA1' }),
    ]);
    render(<StatsDialog open onClose={() => {}} />);
    await screen.findByText('Flights');
    await userEvent.click(screen.getByRole('tab', { name: /upcoming/i }));
    await waitFor(() => {
      expect(screen.getByText('Flights').nextSibling?.textContent).toBe('1');
    });
  });

  it('shows the cancelled/diverted footer when applicable', async () => {
    h.api.listFlights.mockResolvedValue([
      flight({ id: 1, status: 'Cancelled' }),
      flight({ id: 2, status: 'Diverted' }),
    ]);
    render(<StatsDialog open onClose={() => {}} />);
    expect(
      await screen.findByText(/2 cancelled\/diverted flights not counted/i),
    ).toBeInTheDocument();
  });

  it('hides the cancelled/diverted footer when there are none', async () => {
    h.api.listFlights.mockResolvedValue([flight()]);
    render(<StatsDialog open onClose={() => {}} />);
    await screen.findByText('Flights');
    expect(screen.queryByText(/cancelled\/diverted/i)).toBeNull();
  });

  it('renders highlights from flown flights', async () => {
    h.api.listFlights.mockResolvedValue([
      flight({ id: 1, ident: 'BA286' }),
      flight({ id: 2, ident: 'BA999', origin_iata: 'LHR', dest_iata: 'JFK', dest_lat: 40.6413, dest_lon: -73.7781 }),
    ]);
    render(<StatsDialog open onClose={() => {}} />);
    expect(await screen.findByText('Longest flight')).toBeInTheDocument();
    expect(screen.getByText('Most-visited airport')).toBeInTheDocument();
    expect(screen.getByText('Distinct airlines').nextSibling?.textContent).toBe('1');
  });

  it('hides the Around the Earth tile when the ratio is < 0.1', async () => {
    // Short flight: SFO → LAX ≈ 337 mi
    h.api.listFlights.mockResolvedValue([
      flight({ dest_iata: 'LAX', dest_lat: 33.9425, dest_lon: -118.4081 }),
    ]);
    render(<StatsDialog open onClose={() => {}} />);
    await screen.findByText('Longest flight');
    expect(screen.queryByText(/around the earth/i)).toBeNull();
  });

  it('shows Around the Earth when the ratio is >= 0.1', async () => {
    h.api.listFlights.mockResolvedValue([flight()]); // SFO→LHR ≈ 0.22×
    render(<StatsDialog open onClose={() => {}} />);
    expect(await screen.findByText(/around the earth/i)).toBeInTheDocument();
    expect(screen.getByText(/0\.2× laps/)).toBeInTheDocument();
  });

  it('shows an error alert when the fetch fails', async () => {
    h.api.listFlights.mockRejectedValueOnce(new Error('boom'));
    render(<StatsDialog open onClose={() => {}} />);
    expect(await screen.findByRole('alert')).toHaveTextContent('boom');
  });

  it('re-fetches when reopened', async () => {
    const { rerender } = render(<StatsDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.api.listFlights).toHaveBeenCalledTimes(1));
    rerender(<StatsDialog open={false} onClose={() => {}} />);
    rerender(<StatsDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.api.listFlights).toHaveBeenCalledTimes(2));
  });
});

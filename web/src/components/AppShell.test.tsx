import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { User } from '../api/types';
import { setMatchMedia } from '../test/setup';

const h = vi.hoisted(() => ({
  state: {
    me: null as User | null,
    logout: vi.fn(),
  },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof h.state) => unknown) => sel(h.state),
}));
vi.mock('./FlightList', () => ({
  default: ({ onEditFlight }: { onEditFlight: (id: number) => void }) => (
    <button onClick={() => onEditFlight(42)}>EDIT_FROM_LIST</button>
  ),
}));
vi.mock('./FlightMap', () => ({ default: () => <div>FLIGHT_MAP</div> }));
vi.mock('./FlightDialog', () => ({
  default: ({
    open,
    editId,
    onClose,
  }: {
    open: boolean;
    editId: number | null;
    onClose: () => void;
  }) =>
    open ? (
      <div>
        FLIGHT_DIALOG editId={String(editId)}
        <button onClick={onClose}>CLOSE_FLIGHT_DIALOG</button>
      </div>
    ) : null,
}));
vi.mock('./AdminDialog', () => ({
  default: ({ open, onClose }: { open: boolean; onClose: () => void }) =>
    open ? (
      <div>
        ADMIN_DIALOG
        <button onClick={onClose}>CLOSE_ADMIN_DIALOG</button>
      </div>
    ) : null,
}));

import AppShell from './AppShell';

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
  h.state.me = user();
  setMatchMedia(false);
});

describe('AppShell', () => {
  it('hides the admin button for non-superusers (wide layout)', () => {
    h.state.me = user({ is_superuser: false });
    render(<AppShell />);
    expect(screen.queryByLabelText(/manage users/i)).not.toBeInTheDocument();
    expect(screen.getByText('FLIGHT_MAP')).toBeInTheDocument();
  });

  it('shows the admin button for superusers and opens AdminDialog', async () => {
    h.state.me = user({ is_superuser: true });
    render(<AppShell />);
    const adminBtn = screen.getByRole('button', { name: /manage users/i });
    await userEvent.click(adminBtn);
    expect(screen.getByText('ADMIN_DIALOG')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'CLOSE_ADMIN_DIALOG' }));
    expect(screen.queryByText('ADMIN_DIALOG')).not.toBeInTheDocument();
  });

  it('opens and closes the FlightDialog from the Add flight button', async () => {
    render(<AppShell />);
    await userEvent.click(screen.getByRole('button', { name: /add flight/i }));
    expect(screen.getByText(/FLIGHT_DIALOG editId=null/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'CLOSE_FLIGHT_DIALOG' }));
    expect(screen.queryByText(/FLIGHT_DIALOG/)).not.toBeInTheDocument();
  });

  it('opens the FlightDialog in edit mode from the list', async () => {
    render(<AppShell />);
    await userEvent.click(screen.getByText('EDIT_FROM_LIST'));
    expect(screen.getByText(/FLIGHT_DIALOG editId=42/)).toBeInTheDocument();
  });

  it('logs out via the logout button', async () => {
    render(<AppShell />);
    // The logout IconButton is the one inside the user tooltip.
    const buttons = screen.getAllByRole('button');
    // Last button is the sidebar toggle; logout is the small icon next to avatar.
    const logoutBtn = buttons.find((b) => b.querySelector('[data-testid="LogoutIcon"]'));
    await userEvent.click(logoutBtn!);
    expect(h.state.logout).toHaveBeenCalled();
  });

  it('toggles the sidebar', async () => {
    render(<AppShell />);
    const toggle = screen.getByRole('button', { name: /hide flight list/i });
    await userEvent.click(toggle);
    expect(screen.getByRole('button', { name: /show flight list/i })).toBeInTheDocument();
  });

  it('starts with sidebar closed on narrow screens', () => {
    setMatchMedia(true); // useMediaQuery(down('sm')) -> true (narrow)
    render(<AppShell />);
    expect(screen.getByRole('button', { name: /show flight list/i })).toBeInTheDocument();
  });

  it('dispatches a window resize after the sidebar transition (resize effect)', () => {
    vi.useFakeTimers();
    const resizeSpy = vi.fn();
    window.addEventListener('resize', resizeSpy);
    render(<AppShell />);
    act(() => {
      vi.advanceTimersByTime(250);
    });
    window.removeEventListener('resize', resizeSpy);
    vi.useRealTimers();
    expect(resizeSpy).toHaveBeenCalled();
  });

  it('renders fallback avatar initial when me is null', () => {
    h.state.me = null;
    render(<AppShell />);
    expect(screen.getByText('Flight Tracker')).toBeInTheDocument();
  });
});

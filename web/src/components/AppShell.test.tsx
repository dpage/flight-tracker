import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { User } from '../api/types';
import { setMatchMedia } from '../test/setup';
import { setThemePreference, THEME_STORAGE_KEY } from '../theme';

const h = vi.hoisted(() => ({
  state: {
    me: null as User | null,
    logout: vi.fn(),
    capabilities: { resolver_available: false, poll_interval_sec: 60, email_ingest_enabled: false },
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
vi.mock('./EmailsDialog', () => ({
  default: ({ open, onClose }: { open: boolean; onClose: () => void }) =>
    open ? (
      <div>
        EMAILS_DIALOG
        <button onClick={onClose}>CLOSE_EMAILS_DIALOG</button>
      </div>
    ) : null,
}));
vi.mock('./FriendsDialog', () => ({
  default: ({ open, onClose }: { open: boolean; onClose: () => void }) =>
    open ? (
      <div>
        FRIENDS_DIALOG
        <button onClick={onClose}>CLOSE_FRIENDS_DIALOG</button>
      </div>
    ) : null,
}));
vi.mock('./StatsDialog', () => ({
  default: ({ open, onClose }: { open: boolean; onClose: () => void }) =>
    open ? (
      <div>
        STATS_DIALOG
        <button onClick={onClose}>CLOSE_STATS_DIALOG</button>
      </div>
    ) : null,
}));

import AppShell from './AppShell';

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

beforeEach(() => {
  vi.clearAllMocks();
  h.state.me = user();
  setMatchMedia(false);
  localStorage.clear();
  setThemePreference('system');
  localStorage.clear();
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

  it('logs out from the avatar menu', async () => {
    render(<AppShell />);
    await userEvent.click(screen.getByRole('button', { name: /account menu/i }));
    await userEvent.click(screen.getByRole('menuitem', { name: /sign out/i }));
    expect(h.state.logout).toHaveBeenCalled();
  });

  it('does not show Email addresses when ingest is disabled', async () => {
    h.state.capabilities = {
      resolver_available: false,
      poll_interval_sec: 60,
      email_ingest_enabled: false,
    };
    render(<AppShell />);
    await userEvent.click(screen.getByRole('button', { name: /account menu/i }));
    expect(screen.queryByRole('menuitem', { name: /email addresses/i })).not.toBeInTheDocument();
  });

  it('opens EmailsDialog from the avatar menu when ingest is enabled', async () => {
    h.state.capabilities = {
      resolver_available: false,
      poll_interval_sec: 60,
      email_ingest_enabled: true,
    };
    render(<AppShell />);
    await userEvent.click(screen.getByRole('button', { name: /account menu/i }));
    await userEvent.click(screen.getByRole('menuitem', { name: /email addresses/i }));
    expect(screen.getByText('EMAILS_DIALOG')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'CLOSE_EMAILS_DIALOG' }));
    expect(screen.queryByText('EMAILS_DIALOG')).not.toBeInTheDocument();
  });

  it('opens FriendsDialog from the avatar menu', async () => {
    render(<AppShell />);
    await userEvent.click(screen.getByRole('button', { name: /account menu/i }));
    await userEvent.click(screen.getByRole('menuitem', { name: /^friends/i }));
    expect(screen.getByText('FRIENDS_DIALOG')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'CLOSE_FRIENDS_DIALOG' }));
    expect(screen.queryByText('FRIENDS_DIALOG')).not.toBeInTheDocument();
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
    expect(screen.getByText('Aerly')).toBeInTheDocument();
  });

  it('persists the chosen appearance preference to localStorage', async () => {
    render(<AppShell />);
    await userEvent.click(screen.getByRole('button', { name: /account menu/i }));
    await userEvent.click(screen.getByRole('menuitem', { name: /^dark$/i }));
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
  });

  it('exposes Light, Dark and System options in the appearance picker', async () => {
    render(<AppShell />);
    await userEvent.click(screen.getByRole('button', { name: /account menu/i }));
    expect(screen.getByRole('menuitem', { name: /^light$/i })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: /^dark$/i })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: /^system$/i })).toBeInTheDocument();
  });

  it('opens the Stats dialog from the avatar menu', async () => {
    render(<AppShell />);
    await userEvent.click(screen.getByRole('button', { name: /account menu/i }));
    await userEvent.click(screen.getByRole('menuitem', { name: /statistics/i }));
    expect(screen.getByText('STATS_DIALOG')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'CLOSE_STATS_DIALOG' }));
    expect(screen.queryByText('STATS_DIALOG')).not.toBeInTheDocument();
  });
});

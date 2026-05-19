import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { User } from '../api/types';
import { setMatchMedia } from '../test/setup';

const h = vi.hoisted(() => ({
  state: {
    users: [] as User[],
    me: null as User | null,
    inviteUser: vi.fn(),
    updateUser: vi.fn(),
    deleteUser: vi.fn(),
    setError: vi.fn(),
  },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof h.state) => unknown) => sel(h.state),
}));

import AdminDialog from './AdminDialog';

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
  h.state.users = [];
  h.state.me = user({ id: 1 });
  setMatchMedia(false);
});

describe('AdminDialog wide (table) layout', () => {
  it('renders nothing meaningful when closed', () => {
    render(<AdminDialog open={false} onClose={vi.fn()} />);
    expect(screen.queryByText('Manage users')).not.toBeInTheDocument();
  });

  it('renders the table with you/invited chips, name, and last sign-in', () => {
    h.state.me = user({ id: 1 });
    h.state.users = [
      user({ id: 1, github_login: 'me', name: 'My Name', last_login_at: '2024-01-01T10:00:00Z' }),
      user({ id: 2, github_login: 'newbie', name: '', has_logged_in: false }),
    ];
    render(<AdminDialog open onClose={vi.fn()} />);
    expect(screen.getByText('you')).toBeInTheDocument();
    expect(screen.getByText('invited')).toBeInTheDocument();
    expect(screen.getByText('My Name')).toBeInTheDocument();
    // last_login_at present -> a localized date string; absent -> em dash
    expect(screen.getByText('—')).toBeInTheDocument();
  });

  it('invite success clears the fields', async () => {
    h.state.inviteUser.mockResolvedValue(undefined);
    render(<AdminDialog open onClose={vi.fn()} />);
    const login = screen.getByLabelText(/github login/i);
    const name = screen.getByLabelText(/display name/i);
    await userEvent.type(login, 'octo');
    await userEvent.type(name, 'Octo Cat');
    await userEvent.click(screen.getByLabelText('Superuser'));
    await userEvent.click(screen.getByRole('button', { name: /invite/i }));
    expect(h.state.inviteUser).toHaveBeenCalledWith({
      github_login: 'octo',
      name: 'Octo Cat',
      is_superuser: true,
    });
    expect((login as HTMLInputElement).value).toBe('');
  });

  it('invite does nothing for blank login', async () => {
    render(<AdminDialog open onClose={vi.fn()} />);
    const btn = screen.getByRole('button', { name: /invite/i });
    expect(btn).toBeDisabled();
  });

  it('invite error surfaces via setError', async () => {
    h.state.inviteUser.mockRejectedValue(new Error('dup user'));
    render(<AdminDialog open onClose={vi.fn()} />);
    await userEvent.type(screen.getByLabelText(/github login/i), 'octo');
    await userEvent.click(screen.getByRole('button', { name: /invite/i }));
    expect(h.state.setError).toHaveBeenCalledWith('dup user');
  });

  it('invite error with non-Error uses String()', async () => {
    h.state.inviteUser.mockRejectedValue('weird');
    render(<AdminDialog open onClose={vi.fn()} />);
    await userEvent.type(screen.getByLabelText(/github login/i), 'octo');
    await userEvent.click(screen.getByRole('button', { name: /invite/i }));
    expect(h.state.setError).toHaveBeenCalledWith('weird');
  });

  it('toggles superuser/active successfully', async () => {
    h.state.me = user({ id: 99 });
    h.state.users = [user({ id: 2, github_login: 'bob', is_superuser: false, is_active: true })];
    h.state.updateUser.mockResolvedValue(undefined);
    render(<AdminDialog open onClose={vi.fn()} />);
    const bobRow = screen.getAllByRole('row').find((r) => within(r).queryByText('bob'))!;
    const switches = within(bobRow).getAllByRole('checkbox');
    await userEvent.click(switches[0]); // superuser
    await userEvent.click(switches[1]); // active
    expect(h.state.updateUser).toHaveBeenCalledWith(2, { is_superuser: true });
    expect(h.state.updateUser).toHaveBeenCalledWith(2, { is_active: false });
  });

  it('toggle superuser error is caught into setError', async () => {
    h.state.me = user({ id: 99 });
    h.state.users = [user({ id: 2, github_login: 'bob' })];
    h.state.updateUser.mockRejectedValueOnce(new Error('nope'));
    render(<AdminDialog open onClose={vi.fn()} />);
    const bobRow = screen.getAllByRole('row').find((r) => within(r).queryByText('bob'))!;
    const switches = within(bobRow).getAllByRole('checkbox');
    await userEvent.click(switches[0]);
    await Promise.resolve();
    expect(h.state.setError).toHaveBeenCalledWith('nope');
  });

  it('toggle active error (non-Error) is caught into setError', async () => {
    h.state.me = user({ id: 99 });
    h.state.users = [user({ id: 2, github_login: 'bob' })];
    h.state.updateUser.mockRejectedValueOnce('strfail');
    render(<AdminDialog open onClose={vi.fn()} />);
    const bobRow = screen.getAllByRole('row').find((r) => within(r).queryByText('bob'))!;
    const switches = within(bobRow).getAllByRole('checkbox');
    await userEvent.click(switches[1]);
    await Promise.resolve();
    expect(h.state.setError).toHaveBeenCalledWith('strfail');
  });

  it('delete confirm true deletes, false does not; isMe disables', async () => {
    h.state.me = user({ id: 1, github_login: 'me' });
    h.state.users = [user({ id: 1, github_login: 'me' }), user({ id: 2, github_login: 'bob' })];
    h.state.deleteUser.mockResolvedValue(undefined);
    render(<AdminDialog open onClose={vi.fn()} />);
    const rows = screen.getAllByRole('row');
    // bob's row delete button
    const bobRow = rows.find((r) => within(r).queryByText('bob'))!;
    const delBtn = within(bobRow).getAllByRole('button').slice(-1)[0];

    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);
    await userEvent.click(delBtn);
    expect(h.state.deleteUser).not.toHaveBeenCalled();
    confirmSpy.mockReturnValue(true);
    await userEvent.click(delBtn);
    expect(h.state.deleteUser).toHaveBeenCalledWith(2);
    confirmSpy.mockRestore();

    // my own row delete button is disabled
    const myRow = rows.find((r) => within(r).queryByText('me'))!;
    const myDel = within(myRow).getAllByRole('button').slice(-1)[0];
    expect(myDel).toBeDisabled();
  });

  it('calls onClose from the Close button', async () => {
    const onClose = vi.fn();
    render(<AdminDialog open onClose={onClose} />);
    await userEvent.click(screen.getByRole('button', { name: /close/i }));
    expect(onClose).toHaveBeenCalled();
  });
});

describe('AdminDialog narrow (UserCard) layout', () => {
  beforeEach(() => setMatchMedia(true));

  it('renders UserCard with chips, name, formatted last sign-in', () => {
    h.state.me = user({ id: 1, github_login: 'me' });
    h.state.users = [
      user({ id: 1, github_login: 'me', name: 'My Name', last_login_at: '2024-06-01T08:30:00Z' }),
      user({ id: 2, github_login: 'newbie', name: '', has_logged_in: false }),
    ];
    render(<AdminDialog open onClose={vi.fn()} />);
    expect(screen.getByText('you')).toBeInTheDocument();
    expect(screen.getByText('invited')).toBeInTheDocument();
    expect(screen.getByText('My Name')).toBeInTheDocument();
    // "Last sign-in: —" is split across text nodes for the invited user.
    expect(
      screen.getAllByText((_, node) => node?.textContent === 'Last sign-in: —').length,
    ).toBeGreaterThan(0);
  });

  it('UserCard toggles + delete work', async () => {
    h.state.me = user({ id: 99 });
    h.state.users = [user({ id: 2, github_login: 'bob' })];
    h.state.updateUser.mockResolvedValue(undefined);
    h.state.deleteUser.mockResolvedValue(undefined);
    render(<AdminDialog open onClose={vi.fn()} />);
    // checkbox[0] is the invite-form "Superuser" switch; the card adds two more.
    const switches = screen.getAllByRole('checkbox');
    await userEvent.click(switches[1]); // card superuser
    await userEvent.click(switches[2]); // card active
    expect(h.state.updateUser).toHaveBeenCalledWith(2, { is_superuser: true });
    expect(h.state.updateUser).toHaveBeenCalledWith(2, { is_active: false });

    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    const delBtn = screen.getAllByRole('button').find((b) =>
      b.querySelector('[data-testid="DeleteOutlineIcon"]'),
    )!;
    await userEvent.click(delBtn);
    expect(h.state.deleteUser).toHaveBeenCalledWith(2);
    confirmSpy.mockRestore();
  });
});

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Friendship, User } from '../api/types';

const h = vi.hoisted(() => ({
  api: {
    listFriends: vi.fn(),
    inviteFriend: vi.fn(),
    acceptFriend: vi.fn(),
    removeFriend: vi.fn(),
  },
  setError: vi.fn(),
  users: [] as User[],
}));

vi.mock('../api/client', () => ({ api: h.api }));
vi.mock('../state/store', () => ({
  useStore: (sel: (s: { setError: (m: string | null) => void; users: User[] }) => unknown) =>
    sel({ setError: h.setError, users: h.users }),
}));

import FriendsDialog from './FriendsDialog';

function user(over: Partial<User> = {}): User {
  return {
    id: 1,
    username: 'alice',
    name: 'Alice',
    avatar_url: '',
    is_superuser: false,
    is_active: true,
    has_logged_in: true,
    ...over,
  };
}

function friend(over: Partial<Friendship> = {}): Friendship {
  return {
    friend_id: 2,
    status: 'accepted',
    requested_at: new Date().toISOString(),
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  h.api.listFriends.mockResolvedValue([]);
  h.users = [user({ id: 2, username: 'bob', name: 'Bob' })];
});

describe('FriendsDialog', () => {
  it('lists current friends with their status when opened', async () => {
    h.api.listFriends.mockResolvedValue([
      friend({ friend_id: 2, status: 'accepted' }),
      friend({ friend_id: 3, status: 'pending', direction: 'incoming' }),
      friend({ friend_id: 4, status: 'pending', direction: 'outgoing' }),
    ]);
    h.users = [
      user({ id: 2, username: 'bob', name: 'Bob' }),
      user({ id: 3, username: 'carol', name: 'Carol' }),
      user({ id: 4, username: 'dan', name: 'Dan' }),
    ];
    render(<FriendsDialog open onClose={() => {}} />);
    await screen.findByText('Bob');
    expect(screen.getByText('Carol')).toBeInTheDocument();
    expect(screen.getByText('Dan')).toBeInTheDocument();
    expect(screen.getByText(/accepted/i)).toBeInTheDocument();
    expect(screen.getByText(/wants to friend you/i)).toBeInTheDocument();
    expect(screen.getByText(/invite sent/i)).toBeInTheDocument();
  });

  it('sends an invite with the optional message, then dismisses the success alert', async () => {
    h.api.listFriends.mockResolvedValueOnce([]);
    h.api.inviteFriend.mockResolvedValueOnce(undefined);
    h.api.listFriends.mockResolvedValueOnce([
      friend({ friend_id: 2, status: 'pending', direction: 'outgoing' }),
    ]);
    render(<FriendsDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.api.listFriends).toHaveBeenCalledTimes(1));

    await userEvent.type(screen.getByLabelText(/email address/i), 'bob@example.com');
    await userEvent.type(screen.getByLabelText(/add a message/i), 'come join us');
    await userEvent.click(screen.getByRole('button', { name: /^invite$/i }));

    await waitFor(() =>
      expect(h.api.inviteFriend).toHaveBeenCalledWith({
        email: 'bob@example.com',
        message: 'come join us',
      }),
    );
    const alert = await screen.findByText(
      /if bob@example.com is on aerly we sent them a friend request/i,
    );
    expect(await screen.findByText(/invite sent/i)).toBeInTheDocument();

    // Close the success Alert via its onClose handler.
    const closeBtn = alert.closest('.MuiAlert-root')?.querySelector('button');
    expect(closeBtn).toBeTruthy();
    await userEvent.click(closeBtn!);
    expect(
      screen.queryByText(/if bob@example.com is on aerly we sent them a friend request/i),
    ).not.toBeInTheDocument();
  });

  it('accepts an incoming pending request and leaves siblings untouched', async () => {
    // Two incoming requests so the map callback inside handleAccept hits
    // both the matched and unmatched branches.
    h.users = [
      user({ id: 2, username: 'bob', name: 'Bob' }),
      user({ id: 3, username: 'carol', name: 'Carol' }),
    ];
    h.api.listFriends.mockResolvedValue([
      friend({ friend_id: 2, status: 'pending', direction: 'incoming' }),
      friend({ friend_id: 3, status: 'pending', direction: 'incoming' }),
    ]);
    h.api.acceptFriend.mockResolvedValueOnce(friend({ friend_id: 2, status: 'accepted' }));
    render(<FriendsDialog open onClose={() => {}} />);
    const acceptBtn = await screen.findByRole('button', { name: /accept bob/i });
    await userEvent.click(acceptBtn);
    await waitFor(() => expect(h.api.acceptFriend).toHaveBeenCalledWith(2));
    // Bob is now accepted; Carol's pending row should still be pending.
    expect(await screen.findByText(/^accepted$/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /accept carol/i })).toBeInTheDocument();
  });

  it('unfriends an accepted friend after confirmation, leaving siblings', async () => {
    // Two accepted friends so the filter inside handleRemove exercises
    // both the keep and drop branches.
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    h.users = [
      user({ id: 2, username: 'bob', name: 'Bob' }),
      user({ id: 3, username: 'carol', name: 'Carol' }),
    ];
    h.api.listFriends.mockResolvedValue([
      friend({ friend_id: 2, status: 'accepted' }),
      friend({ friend_id: 3, status: 'accepted' }),
    ]);
    h.api.removeFriend.mockResolvedValueOnce(undefined);
    render(<FriendsDialog open onClose={() => {}} />);
    const removeBtn = await screen.findByRole('button', { name: /remove bob/i });
    await userEvent.click(removeBtn);
    expect(confirmSpy).toHaveBeenCalled();
    await waitFor(() => expect(h.api.removeFriend).toHaveBeenCalledWith(2));
    expect(screen.queryByText('Bob')).not.toBeInTheDocument();
    expect(screen.getByText('Carol')).toBeInTheDocument();
    confirmSpy.mockRestore();
  });

  it('skips the listFriends fetch when rendered with open=false', () => {
    render(<FriendsDialog open={false} onClose={() => {}} />);
    expect(h.api.listFriends).not.toHaveBeenCalled();
  });

  it('renders the empty state when the user has no friends yet', async () => {
    h.api.listFriends.mockResolvedValue([]);
    render(<FriendsDialog open onClose={() => {}} />);
    await screen.findByText(/you don't have any friends on aerly yet/i);
  });

  it("falls back to User #N when the global user list doesn't include a friend", async () => {
    // /api/users hasn't loaded the friend yet, so userIndex.get returns
    // undefined and friendLabel hits the fallback branch.
    h.api.listFriends.mockResolvedValue([friend({ friend_id: 99, status: 'accepted' })]);
    h.users = [];
    render(<FriendsDialog open onClose={() => {}} />);
    expect(await screen.findByText('User #99')).toBeInTheDocument();
  });

  it("trims an empty invite email instead of calling the server", async () => {
    h.api.listFriends.mockResolvedValue([]);
    render(<FriendsDialog open onClose={() => {}} />);
    // Whitespace-only email keeps the button disabled, so we can't click
    // it via the UI; instead we type whitespace then assert the disabled
    // state to prove the early-return path doesn't issue an API call.
    await userEvent.type(screen.getByLabelText(/email address/i), '   ');
    expect(screen.getByRole('button', { name: /invite/i })).toBeDisabled();
    expect(h.api.inviteFriend).not.toHaveBeenCalled();
  });

  it('reports listFriends errors via setError', async () => {
    h.api.listFriends.mockRejectedValueOnce(new Error('boom-list'));
    render(<FriendsDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('boom-list'));
  });

  it('reports inviteFriend errors via setError', async () => {
    h.api.listFriends.mockResolvedValueOnce([]);
    h.api.inviteFriend.mockRejectedValueOnce('plain-string-error');
    render(<FriendsDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.api.listFriends).toHaveBeenCalledTimes(1));
    await userEvent.type(screen.getByLabelText(/email address/i), 'bob@example.com');
    await userEvent.click(screen.getByRole('button', { name: /invite/i }));
    // A non-Error rejection should stringify, not crash.
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('plain-string-error'));
  });

  it('reports acceptFriend errors via setError', async () => {
    h.api.listFriends.mockResolvedValue([
      friend({ friend_id: 2, status: 'pending', direction: 'incoming' }),
    ]);
    h.api.acceptFriend.mockRejectedValueOnce(new Error('accept-failed'));
    render(<FriendsDialog open onClose={() => {}} />);
    const acceptBtn = await screen.findByRole('button', { name: /accept bob/i });
    await userEvent.click(acceptBtn);
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('accept-failed'));
  });

  it('does not call removeFriend when the user cancels the confirm prompt', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);
    h.api.listFriends.mockResolvedValue([friend({ friend_id: 2, status: 'accepted' })]);
    render(<FriendsDialog open onClose={() => {}} />);
    const removeBtn = await screen.findByRole('button', { name: /remove bob/i });
    await userEvent.click(removeBtn);
    expect(confirmSpy).toHaveBeenCalled();
    expect(h.api.removeFriend).not.toHaveBeenCalled();
    confirmSpy.mockRestore();
  });

  it('reports removeFriend errors via setError', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    h.api.listFriends.mockResolvedValue([friend({ friend_id: 2, status: 'accepted' })]);
    h.api.removeFriend.mockRejectedValueOnce(new Error('rm-failed'));
    render(<FriendsDialog open onClose={() => {}} />);
    const removeBtn = await screen.findByRole('button', { name: /remove bob/i });
    await userEvent.click(removeBtn);
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('rm-failed'));
    // Bob should still be in the list — the removal didn't actually happen.
    expect(screen.getByText('Bob')).toBeInTheDocument();
    confirmSpy.mockRestore();
  });

  it("renders the outgoing-pending row's cancel button (separate from unfriend)", async () => {
    h.api.listFriends.mockResolvedValue([
      friend({ friend_id: 2, status: 'pending', direction: 'outgoing' }),
    ]);
    render(<FriendsDialog open onClose={() => {}} />);
    // Outgoing rows expose the same delete button (different tooltip
    // copy); clicking it goes through handleRemove.
    expect(await screen.findByRole('button', { name: /remove bob/i })).toBeInTheDocument();
  });

  it("renders the incoming-pending row's decline button", async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    h.api.listFriends.mockResolvedValue([
      friend({ friend_id: 2, status: 'pending', direction: 'incoming' }),
    ]);
    h.api.removeFriend.mockResolvedValueOnce(undefined);
    render(<FriendsDialog open onClose={() => {}} />);
    const declineBtn = await screen.findByRole('button', { name: /decline bob/i });
    await userEvent.click(declineBtn);
    await waitFor(() => expect(h.api.removeFriend).toHaveBeenCalledWith(2));
    confirmSpy.mockRestore();
  });

  it('falls back to username when the user has no display name', async () => {
    h.api.listFriends.mockResolvedValue([friend({ friend_id: 5, status: 'accepted' })]);
    h.users = [
      user({ id: 5, username: 'eve', name: '   ' }), // whitespace-only name
    ];
    render(<FriendsDialog open onClose={() => {}} />);
    expect(await screen.findByText('eve')).toBeInTheDocument();
  });
});

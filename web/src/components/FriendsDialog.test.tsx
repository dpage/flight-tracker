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

  it('sends an invite, refreshes the list and shows the no-leak success message', async () => {
    h.api.listFriends.mockResolvedValueOnce([]);
    h.api.inviteFriend.mockResolvedValueOnce(undefined);
    // Second listFriends call after invite reflects the new pending row.
    h.api.listFriends.mockResolvedValueOnce([
      friend({ friend_id: 2, status: 'pending', direction: 'outgoing' }),
    ]);
    render(<FriendsDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.api.listFriends).toHaveBeenCalledTimes(1));

    await userEvent.type(screen.getByLabelText(/email address/i), 'bob@example.com');
    await userEvent.click(screen.getByRole('button', { name: /invite/i }));

    await waitFor(() =>
      expect(h.api.inviteFriend).toHaveBeenCalledWith({ email: 'bob@example.com' }),
    );
    await screen.findByText(/if bob@example.com is on aerly we sent them a friend request/i);
    expect(await screen.findByText(/invite sent/i)).toBeInTheDocument();
  });

  it('accepts an incoming pending request', async () => {
    h.api.listFriends.mockResolvedValue([
      friend({ friend_id: 2, status: 'pending', direction: 'incoming' }),
    ]);
    h.api.acceptFriend.mockResolvedValueOnce(friend({ friend_id: 2, status: 'accepted' }));
    render(<FriendsDialog open onClose={() => {}} />);
    const acceptBtn = await screen.findByRole('button', { name: /accept bob/i });
    await userEvent.click(acceptBtn);
    await waitFor(() => expect(h.api.acceptFriend).toHaveBeenCalledWith(2));
    expect(await screen.findByText(/accepted/i)).toBeInTheDocument();
  });

  it('unfriends an accepted friend after confirmation', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    h.api.listFriends.mockResolvedValue([friend({ friend_id: 2, status: 'accepted' })]);
    h.api.removeFriend.mockResolvedValueOnce(undefined);
    render(<FriendsDialog open onClose={() => {}} />);
    const removeBtn = await screen.findByRole('button', { name: /remove bob/i });
    await userEvent.click(removeBtn);
    expect(confirmSpy).toHaveBeenCalled();
    await waitFor(() => expect(h.api.removeFriend).toHaveBeenCalledWith(2));
    expect(screen.queryByText('Bob')).not.toBeInTheDocument();
    confirmSpy.mockRestore();
  });

  it('renders the empty state when the user has no friends yet', async () => {
    h.api.listFriends.mockResolvedValue([]);
    render(<FriendsDialog open onClose={() => {}} />);
    await screen.findByText(/you don't have any friends on aerly yet/i);
  });
});

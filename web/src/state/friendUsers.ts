import { useMemo } from 'react';

import { useStore } from './store';
import type { User } from '../api/types';

/**
 * Returns the subset of loaded users that the signed-in viewer has an
 * accepted friendship with. Used by share/passenger pickers to limit
 * options to friends. Returns [] while `me` is unknown.
 */
export function useFriendUsers(): User[] {
  const meId = useStore((s) => s.me?.id);
  const users = useStore((s) => s.users);
  const friendships = useStore((s) => s.friendships);

  return useMemo(() => {
    if (meId == null) return [];
    const friendIds = new Set<number>();
    for (const f of friendships) {
      // friend_id is omitted for outgoing pending invites whose target
      // hasn't joined yet — they're carried by invited_email instead and
      // can't be picked as flight passengers/sharees regardless.
      if (f.status === 'accepted' && f.friend_id != null) {
        friendIds.add(f.friend_id);
      }
    }
    return users.filter((u) => friendIds.has(u.id));
  }, [meId, users, friendships]);
}

import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Avatar,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import CheckIcon from '@mui/icons-material/Check';
import CloseIcon from '@mui/icons-material/Close';
import DeleteOutlineIcon from '@mui/icons-material/DeleteOutline';

import { api } from '../api/client';
import type { Friendship, User } from '../api/types';
import { useStore } from '../state/store';

interface Props {
  open: boolean;
  onClose: () => void;
}

// The /api/users endpoint returns every user the server knows about; we
// only use it to render display names/avatars for friends. We index it
// once per dialog open.
function buildUserIndex(users: User[]): Map<number, User> {
  const m = new Map<number, User>();
  for (const u of users) m.set(u.id, u);
  return m;
}

export default function FriendsDialog({ open, onClose }: Props) {
  const setError = useStore((s) => s.setError);
  const users = useStore((s) => s.users);
  const userIndex = useMemo(() => buildUserIndex(users), [users]);

  const [friends, setFriends] = useState<Friendship[]>([]);
  const [email, setEmail] = useState('');
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const [inviteFeedback, setInviteFeedback] = useState<string | null>(null);

  const reportError = useCallback(
    (err: unknown) => setError(err instanceof Error ? err.message : String(err)),
    [setError],
  );

  useEffect(() => {
    if (!open) return;
    void api.listFriends().then(setFriends).catch(reportError);
    setInviteFeedback(null);
  }, [open, reportError]);

  const handleInvite = async () => {
    const trimmed = email.trim();
    setBusy(true);
    setInviteFeedback(null);
    try {
      await api.inviteFriend({ email: trimmed, message: message.trim() || undefined });
      // Pull a fresh list so a newly-pending outgoing request shows up.
      const updated = await api.listFriends();
      setFriends(updated);
      setEmail('');
      setMessage('');
      // The server returns identical responses whether the email matched
      // an existing user or got queued; show the same message either way
      // so we don't leak which case applied.
      setInviteFeedback(
        `If ${trimmed} is on Aerly we sent them a friend request; ` +
          `otherwise we emailed an invitation. They'll see it next time they sign in.`,
      );
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleAccept = async (other: number) => {
    try {
      const updated = await api.acceptFriend(other);
      setFriends((rows) => rows.map((r) => (r.friend_id === other ? updated : r)));
    } catch (err) {
      reportError(err);
    }
  };

  const handleRemove = async (other: number, label: string) => {
    if (!window.confirm(`Remove ${label} from your friends?`)) return;
    try {
      await api.removeFriend(other);
      setFriends((rows) => rows.filter((r) => r.friend_id !== other));
    } catch (err) {
      reportError(err);
    }
  };

  const friendLabel = (id: number): string => {
    const u = userIndex.get(id);
    if (!u) return `User #${id}`;
    return u.name?.trim() || u.username;
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Friends</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={3}>
          <Box>
            <Typography variant="subtitle2" sx={{ mb: 1 }}>
              Add a friend by email
            </Typography>
            <Stack direction="row" spacing={1} alignItems="center">
              <TextField
                label="Email address"
                size="small"
                fullWidth
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                type="email"
              />
              <Button
                variant="contained"
                onClick={() => void handleInvite()}
                disabled={busy || email.trim() === ''}
              >
                Invite
              </Button>
            </Stack>
            <TextField
              label="Add a message (optional)"
              size="small"
              fullWidth
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              multiline
              maxRows={3}
              sx={{ mt: 1 }}
            />
            {inviteFeedback && (
              <Alert severity="success" sx={{ mt: 1.5 }} onClose={() => setInviteFeedback(null)}>
                {inviteFeedback}
              </Alert>
            )}
          </Box>

          {friends.length === 0 ? (
            <Typography variant="body2" color="text.secondary">
              You don't have any friends on Aerly yet. Invite someone by their email above.
            </Typography>
          ) : (
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>Friend</TableCell>
                  <TableCell align="center">Status</TableCell>
                  <TableCell align="right" />
                </TableRow>
              </TableHead>
              <TableBody>
                {friends.map((f) => {
                  const label = friendLabel(f.friend_id);
                  const user = userIndex.get(f.friend_id);
                  return (
                    <TableRow key={f.friend_id} hover>
                      <TableCell>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                          <Avatar
                            src={user?.avatar_url}
                            sx={{ width: 24, height: 24 }}
                          >
                            {label.charAt(0).toUpperCase()}
                          </Avatar>
                          <span>{label}</span>
                        </Box>
                      </TableCell>
                      <TableCell align="center">
                        {f.status === 'accepted' ? (
                          <Chip label="accepted" size="small" color="success" variant="outlined" />
                        ) : f.direction === 'outgoing' ? (
                          <Chip
                            label="invite sent"
                            size="small"
                            color="warning"
                            variant="outlined"
                          />
                        ) : (
                          <Chip
                            label="wants to friend you"
                            size="small"
                            color="info"
                            variant="outlined"
                          />
                        )}
                      </TableCell>
                      <TableCell align="right">
                        <Box sx={{ display: 'inline-flex', gap: 0.5 }}>
                          {f.status === 'pending' && f.direction === 'incoming' && (
                            <Tooltip title="Accept">
                              <IconButton
                                size="small"
                                aria-label={`Accept ${label}`}
                                onClick={() => void handleAccept(f.friend_id)}
                              >
                                <CheckIcon fontSize="small" />
                              </IconButton>
                            </Tooltip>
                          )}
                          {f.status === 'pending' && f.direction === 'incoming' && (
                            <Tooltip title="Decline">
                              <IconButton
                                size="small"
                                aria-label={`Decline ${label}`}
                                onClick={() => void handleRemove(f.friend_id, label)}
                              >
                                <CloseIcon fontSize="small" />
                              </IconButton>
                            </Tooltip>
                          )}
                          {(f.status === 'accepted' || f.direction === 'outgoing') && (
                            <Tooltip title={f.status === 'accepted' ? 'Unfriend' : 'Cancel'}>
                              <IconButton
                                size="small"
                                aria-label={`Remove ${label}`}
                                onClick={() => void handleRemove(f.friend_id, label)}
                              >
                                <DeleteOutlineIcon fontSize="small" />
                              </IconButton>
                            </Tooltip>
                          )}
                        </Box>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Close</Button>
      </DialogActions>
    </Dialog>
  );
}

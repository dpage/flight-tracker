import { useState } from 'react';
import {
  Avatar,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  FormControlLabel,
  IconButton,
  Stack,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  Tooltip,
  Typography,
  useMediaQuery,
  useTheme,
} from '@mui/material';
import DeleteIcon from '@mui/icons-material/DeleteOutline';

import { useStore } from '../state/store';
import type { User } from '../api/types';

interface Props {
  open: boolean;
  onClose: () => void;
}

export default function AdminDialog({ open, onClose }: Props) {
  const users = useStore((s) => s.users);
  const me = useStore((s) => s.me);
  const inviteUser = useStore((s) => s.inviteUser);
  const updateUser = useStore((s) => s.updateUser);
  const deleteUser = useStore((s) => s.deleteUser);
  const setError = useStore((s) => s.setError);

  const theme = useTheme();
  const isNarrow = useMediaQuery(theme.breakpoints.down('sm'));

  const [username, setUsername] = useState('');
  const [name, setName] = useState('');
  const [makeAdmin, setMakeAdmin] = useState(false);
  const [busy, setBusy] = useState(false);

  const doInvite = async () => {
    if (!username.trim()) return;
    setBusy(true);
    try {
      await inviteUser({ username: username.trim(), name: name.trim(), is_superuser: makeAdmin });
      setUsername('');
      setName('');
      setMakeAdmin(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const onToggleSuperuser = (u: User, value: boolean) =>
    void updateUser(u.id, { is_superuser: value }).catch((err) =>
      setError(err instanceof Error ? err.message : String(err)),
    );
  const onToggleActive = (u: User, value: boolean) =>
    void updateUser(u.id, { is_active: value }).catch((err) =>
      setError(err instanceof Error ? err.message : String(err)),
    );
  const onDelete = (u: User) => {
    if (confirm(`Delete ${u.username}?`)) void deleteUser(u.id);
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle>Manage users</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={3}>
          <Stack
            direction={{ xs: 'column', sm: 'row' }}
            spacing={1}
            alignItems={{ xs: 'stretch', sm: 'center' }}
            flexWrap="wrap"
          >
            <TextField
              label="Username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              size="small"
              fullWidth={isNarrow}
            />
            <TextField
              label="Display name (optional)"
              value={name}
              onChange={(e) => setName(e.target.value)}
              size="small"
              fullWidth={isNarrow}
            />
            <Stack
              direction="row"
              spacing={1}
              alignItems="center"
              justifyContent={{ xs: 'space-between', sm: 'flex-start' }}
            >
              <FormControlLabel
                control={
                  <Switch checked={makeAdmin} onChange={(e) => setMakeAdmin(e.target.checked)} />
                }
                label="Superuser"
              />
              <Button
                variant="contained"
                disabled={busy || !username.trim()}
                onClick={() => void doInvite()}
              >
                Invite
              </Button>
            </Stack>
          </Stack>

          {isNarrow ? (
            <Stack divider={<Divider />} spacing={0}>
              {users.map((u) => (
                <UserCard
                  key={u.id}
                  user={u}
                  isMe={u.id === me?.id}
                  onToggleSuperuser={(v) => onToggleSuperuser(u, v)}
                  onToggleActive={(v) => onToggleActive(u, v)}
                  onDelete={() => onDelete(u)}
                />
              ))}
            </Stack>
          ) : (
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>User</TableCell>
                  <TableCell>Last sign-in</TableCell>
                  <TableCell align="center">Superuser</TableCell>
                  <TableCell align="center">Active</TableCell>
                  <TableCell align="right" />
                </TableRow>
              </TableHead>
              <TableBody>
                {users.map((u) => {
                  const isMe = u.id === me?.id;
                  return (
                    <TableRow key={u.id} hover>
                      <TableCell>
                        <Stack direction="row" spacing={1} alignItems="center">
                          <Avatar src={u.avatar_url} sx={{ width: 28, height: 28 }}>
                            {u.username.charAt(0).toUpperCase()}
                          </Avatar>
                          <Stack>
                            <Typography variant="body2">
                              {u.username}
                              {isMe && (
                                <Chip label="you" size="small" sx={{ ml: 1 }} variant="outlined" />
                              )}
                              {!u.has_logged_in && (
                                <Chip
                                  label="invited"
                                  size="small"
                                  sx={{ ml: 1 }}
                                  color="warning"
                                  variant="outlined"
                                />
                              )}
                            </Typography>
                            {u.name && (
                              <Typography variant="caption" color="text.secondary">
                                {u.name}
                              </Typography>
                            )}
                          </Stack>
                        </Stack>
                      </TableCell>
                      <TableCell>
                        {u.last_login_at ? new Date(u.last_login_at).toLocaleString() : '—'}
                      </TableCell>
                      <TableCell align="center">
                        <Switch
                          checked={u.is_superuser}
                          disabled={isMe}
                          onChange={(e) => onToggleSuperuser(u, e.target.checked)}
                        />
                      </TableCell>
                      <TableCell align="center">
                        <Switch
                          checked={u.is_active}
                          disabled={isMe}
                          onChange={(e) => onToggleActive(u, e.target.checked)}
                        />
                      </TableCell>
                      <TableCell align="right">
                        <Tooltip title={isMe ? 'Cannot delete yourself' : 'Delete'}>
                          <span>
                            <IconButton size="small" disabled={isMe} onClick={() => onDelete(u)}>
                              <DeleteIcon fontSize="small" />
                            </IconButton>
                          </span>
                        </Tooltip>
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

interface UserCardProps {
  user: User;
  isMe: boolean;
  onToggleSuperuser: (value: boolean) => void;
  onToggleActive: (value: boolean) => void;
  onDelete: () => void;
}

function UserCard({ user, isMe, onToggleSuperuser, onToggleActive, onDelete }: UserCardProps) {
  return (
    <Box sx={{ py: 1.25 }}>
      <Stack direction="row" spacing={1.5} alignItems="flex-start">
        <Avatar src={user.avatar_url} sx={{ width: 36, height: 36, mt: 0.25 }}>
          {user.username.charAt(0).toUpperCase()}
        </Avatar>
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Stack direction="row" alignItems="center" spacing={0.75} flexWrap="wrap">
            <Typography variant="body2" sx={{ fontWeight: 600 }} noWrap>
              {user.username}
            </Typography>
            {isMe && <Chip label="you" size="small" variant="outlined" />}
            {!user.has_logged_in && (
              <Chip label="invited" size="small" color="warning" variant="outlined" />
            )}
          </Stack>
          {user.name && (
            <Typography variant="caption" color="text.secondary" display="block" noWrap>
              {user.name}
            </Typography>
          )}
          <Typography variant="caption" color="text.secondary" display="block">
            Last sign-in:{' '}
            {user.last_login_at ? formatShortDateTime(user.last_login_at) : '—'}
          </Typography>
          <Stack direction="row" spacing={1.5} sx={{ mt: 0.5 }}>
            <FormControlLabel
              sx={{ m: 0 }}
              control={
                <Switch
                  size="small"
                  checked={user.is_superuser}
                  disabled={isMe}
                  onChange={(e) => onToggleSuperuser(e.target.checked)}
                />
              }
              label={
                <Typography variant="caption" sx={{ ml: 0.5 }}>
                  Superuser
                </Typography>
              }
            />
            <FormControlLabel
              sx={{ m: 0 }}
              control={
                <Switch
                  size="small"
                  checked={user.is_active}
                  disabled={isMe}
                  onChange={(e) => onToggleActive(e.target.checked)}
                />
              }
              label={
                <Typography variant="caption" sx={{ ml: 0.5 }}>
                  Active
                </Typography>
              }
            />
          </Stack>
        </Box>
        <Tooltip title={isMe ? 'Cannot delete yourself' : 'Delete'}>
          <span>
            <IconButton size="small" disabled={isMe} onClick={onDelete}>
              <DeleteIcon fontSize="small" />
            </IconButton>
          </span>
        </Tooltip>
      </Stack>
    </Box>
  );
}

function formatShortDateTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleString(undefined, {
    day: '2-digit',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  });
}

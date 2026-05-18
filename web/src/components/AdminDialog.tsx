import { useState } from 'react';
import {
  Avatar,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
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
} from '@mui/material';
import DeleteIcon from '@mui/icons-material/DeleteOutline';

import { useStore } from '../state/store';

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

  const [login, setLogin] = useState('');
  const [name, setName] = useState('');
  const [makeAdmin, setMakeAdmin] = useState(false);
  const [busy, setBusy] = useState(false);

  const doInvite = async () => {
    if (!login.trim()) return;
    setBusy(true);
    try {
      await inviteUser({ github_login: login.trim(), name: name.trim(), is_superuser: makeAdmin });
      setLogin('');
      setName('');
      setMakeAdmin(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle>Manage users</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={3}>
          <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap">
            <TextField
              label="GitHub login"
              value={login}
              onChange={(e) => setLogin(e.target.value)}
              size="small"
            />
            <TextField
              label="Display name (optional)"
              value={name}
              onChange={(e) => setName(e.target.value)}
              size="small"
            />
            <FormControlLabel
              control={<Switch checked={makeAdmin} onChange={(e) => setMakeAdmin(e.target.checked)} />}
              label="Superuser"
            />
            <Button variant="contained" disabled={busy || !login.trim()} onClick={() => void doInvite()}>
              Invite
            </Button>
          </Stack>

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
                          {u.github_login.charAt(0).toUpperCase()}
                        </Avatar>
                        <Stack>
                          <Typography variant="body2">
                            {u.github_login}
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
                        onChange={(e) =>
                          void updateUser(u.id, { is_superuser: e.target.checked }).catch((err) =>
                            setError(err instanceof Error ? err.message : String(err)),
                          )
                        }
                      />
                    </TableCell>
                    <TableCell align="center">
                      <Switch
                        checked={u.is_active}
                        disabled={isMe}
                        onChange={(e) =>
                          void updateUser(u.id, { is_active: e.target.checked }).catch((err) =>
                            setError(err instanceof Error ? err.message : String(err)),
                          )
                        }
                      />
                    </TableCell>
                    <TableCell align="right">
                      <Tooltip title={isMe ? 'Cannot delete yourself' : 'Delete'}>
                        <span>
                          <IconButton
                            size="small"
                            disabled={isMe}
                            onClick={() => {
                              if (confirm(`Delete ${u.github_login}?`)) void deleteUser(u.id);
                            }}
                          >
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
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Close</Button>
      </DialogActions>
    </Dialog>
  );
}

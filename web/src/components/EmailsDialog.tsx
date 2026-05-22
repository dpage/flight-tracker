import { useEffect, useState } from 'react';
import {
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
import DeleteIcon from '@mui/icons-material/DeleteOutline';
import RefreshIcon from '@mui/icons-material/Refresh';

import { api } from '../api/client';
import type { UserEmail } from '../api/types';
import { useStore } from '../state/store';

interface Props {
  open: boolean;
  onClose: () => void;
}

export default function EmailsDialog({ open, onClose }: Props) {
  const setError = useStore((s) => s.setError);
  const [emails, setEmails] = useState<UserEmail[]>([]);
  const [address, setAddress] = useState('');
  const [busy, setBusy] = useState(false);

  const reportError = (err: unknown) =>
    setError(err instanceof Error ? err.message : String(err));

  useEffect(() => {
    if (!open) return;
    void api.listMyEmails().then(setEmails).catch(reportError);
    // reportError closes over setError, which is stable; intentional dep list.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const handleAdd = async () => {
    const trimmed = address.trim();
    if (!trimmed) return;
    setBusy(true);
    try {
      const created = await api.addMyEmail(trimmed);
      setEmails((rows) => [created, ...rows]);
      setAddress('');
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleDelete = async (row: UserEmail) => {
    if (!window.confirm(`Delete ${row.address}?`)) return;
    try {
      await api.deleteMyEmail(row.id);
      setEmails((rows) => rows.filter((r) => r.id !== row.id));
    } catch (err) {
      reportError(err);
    }
  };

  const handleResend = async (row: UserEmail) => {
    try {
      const updated = await api.resendMyEmail(row.id);
      setEmails((rows) => rows.map((r) => (r.id === row.id ? updated : r)));
    } catch (err) {
      reportError(err);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Manage emails</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={3}>
          <Box>
            <Stack direction="row" spacing={1} alignItems="center">
              <TextField
                label="Email address"
                size="small"
                fullWidth
                value={address}
                onChange={(e) => setAddress(e.target.value)}
              />
              <Button
                variant="contained"
                onClick={() => void handleAdd()}
                disabled={busy || address.trim() === ''}
              >
                Add
              </Button>
            </Stack>
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ display: 'block', mt: 0.5, ml: 1.75 }}
            >
              We'll send a verification link to confirm you own this address.
            </Typography>
          </Box>

          {emails.length === 0 ? (
            <Typography variant="body2" color="text.secondary">
              No addresses registered yet.
            </Typography>
          ) : (
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>Address</TableCell>
                  <TableCell align="center">Status</TableCell>
                  <TableCell align="right" />
                </TableRow>
              </TableHead>
              <TableBody>
                {emails.map((row) => (
                  <TableRow key={row.id} hover>
                    <TableCell>{row.address}</TableCell>
                    <TableCell align="center">
                      {row.verified ? (
                        <Chip label="verified" size="small" color="success" variant="outlined" />
                      ) : (
                        <Chip label="pending" size="small" color="warning" variant="outlined" />
                      )}
                    </TableCell>
                    <TableCell align="right">
                      <Box sx={{ display: 'inline-flex', gap: 0.5 }}>
                        {!row.verified && (
                          <Tooltip title="Resend verification">
                            <IconButton
                              size="small"
                              aria-label={`Resend ${row.address}`}
                              onClick={() => void handleResend(row)}
                            >
                              <RefreshIcon fontSize="small" />
                            </IconButton>
                          </Tooltip>
                        )}
                        <Tooltip title="Delete">
                          <IconButton
                            size="small"
                            aria-label={`Delete ${row.address}`}
                            onClick={() => void handleDelete(row)}
                          >
                            <DeleteIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </Box>
                    </TableCell>
                  </TableRow>
                ))}
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

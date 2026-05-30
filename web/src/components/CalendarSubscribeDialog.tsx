import { useCallback, useEffect, useState } from 'react';
import {
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  Stack,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import CheckIcon from '@mui/icons-material/Check';
import ContentCopyIcon from '@mui/icons-material/ContentCopy';
import RefreshIcon from '@mui/icons-material/Refresh';

import { api } from '../api/client';
import type { CalendarScope, CalendarToken } from '../api/types';

export interface CalendarSubscribeDialogProps {
  open: boolean;
  onClose: () => void;
  /** Which scope this dialog manages. `me` is the personal feed (no id);
   * `trip`/`plan` need the corresponding id. */
  scope: CalendarScope;
  /** Required for `trip`/`plan` scope; ignored for `me`. */
  id?: number;
  title?: string;
}

const SCOPE_BLURB: Record<CalendarScope, string> = {
  me: 'Your whole travel schedule across every trip on your Trips list.',
  trip: 'This single trip, ready to drop into a calendar.',
  plan: 'Just this one entry — changes (like a delayed flight) flow through automatically.',
};

/** Subscribe-from-your-own-calendar UI (PRD §6.7). Surfaces the private iCal
 * feed URL for a traveller / trip / plan scope, with copy-to-clipboard and a
 * regenerate action that revokes the previous link.
 *
 * Talks to the calendar token API directly (it owns no store slice): on open it
 * lists existing tokens to find one for this scope, otherwise the user issues
 * one. Regenerate revokes the old token and issues a fresh URL. */
export default function CalendarSubscribeDialog({
  open,
  onClose,
  scope,
  id,
  title,
}: CalendarSubscribeDialogProps) {
  const [token, setToken] = useState<CalendarToken | null>(null);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  // Find the token already issued for this exact scope (+ id), if any. The
  // `me` feed is scope-only; trip/plan feeds key on id too.
  const matches = useCallback(
    (t: CalendarToken) => t.scope === scope,
    [scope],
  );

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    setToken(null);
    setCopied(false);
    api
      .listCalendarTokens()
      .then((tokens) => {
        if (cancelled) return;
        setToken(tokens.find(matches) ?? null);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(errMsg(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open, matches]);

  const issue = async () => {
    setBusy(true);
    setError(null);
    try {
      const t = await api.issueCalendarToken(scope, scope === 'me' ? undefined : id);
      setToken(t);
    } catch (err) {
      setError(errMsg(err));
    } finally {
      setBusy(false);
    }
  };

  const regenerate = async () => {
    if (!token) return;
    setBusy(true);
    setError(null);
    try {
      // Revoke the old link first so it stops working, then mint a fresh one.
      await api.revokeCalendarToken(token.token);
      const t = await api.issueCalendarToken(scope, scope === 'me' ? undefined : id);
      setToken(t);
      setCopied(false);
    } catch (err) {
      setError(errMsg(err));
    } finally {
      setBusy(false);
    }
  };

  const copy = async () => {
    if (!token) return;
    try {
      await navigator.clipboard.writeText(token.url);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard blocked (insecure context / permissions); the field is
      // still selectable for a manual copy.
    }
  };

  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="sm">
      <DialogTitle>Subscribe to calendar{title ? ` — ${title}` : ''}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" color="text.secondary" gutterBottom>
          {SCOPE_BLURB[scope]}
        </Typography>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 2 }}>
          This is a private, unguessable link tied to you, showing exactly what you can see in the
          app. Add it to Apple Calendar, Google Calendar, or Outlook as a subscribed calendar.
        </Typography>

        {loading ? (
          <Box sx={{ display: 'grid', placeItems: 'center', py: 3 }}>
            <CircularProgress size={24} />
          </Box>
        ) : token ? (
          <Stack spacing={1}>
            <Stack direction="row" spacing={1} alignItems="flex-start">
              <TextField
                value={token.url}
                label="Feed URL"
                fullWidth
                size="small"
                slotProps={{ htmlInput: { readOnly: true, 'aria-label': 'Feed URL' } }}
                onFocus={(e) => e.target.select()}
              />
              <Tooltip title={copied ? 'Copied!' : 'Copy link'}>
                <span>
                  <IconButton onClick={() => void copy()} aria-label="Copy feed URL">
                    {copied ? <CheckIcon color="success" /> : <ContentCopyIcon />}
                  </IconButton>
                </span>
              </Tooltip>
            </Stack>
            <Box>
              <Button
                size="small"
                startIcon={<RefreshIcon />}
                onClick={() => void regenerate()}
                disabled={busy}
              >
                Regenerate link
              </Button>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
                Regenerating revokes the old link — anyone still subscribed to it stops receiving
                updates.
              </Typography>
            </Box>
          </Stack>
        ) : (
          <Button variant="contained" onClick={() => void issue()} disabled={busy}>
            Create feed link
          </Button>
        )}

        {error && (
          <Typography color="error" variant="body2" sx={{ mt: 2 }}>
            {error}
          </Typography>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Close</Button>
      </DialogActions>
    </Dialog>
  );
}

function errMsg(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

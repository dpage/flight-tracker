import { useEffect, useMemo, useRef } from 'react';
import { Alert, Box, CircularProgress, CssBaseline, Snackbar, ThemeProvider } from '@mui/material';
import { LocalizationProvider } from '@mui/x-date-pickers/LocalizationProvider';
import { AdapterDateFns } from '@mui/x-date-pickers/AdapterDateFnsV3';

import { useStore } from './state/store';
import { connectSSE } from './sse';
import { api } from './api/client';
import { createAppTheme, useThemeMode } from './theme';
import Login from './components/Login';
import AppShell from './components/AppShell';
import PrivacyPolicy from './components/PrivacyPolicy';
import TermsOfService from './components/TermsOfService';

export default function App() {
  const auth = useStore((s) => s.auth);
  const init = useStore((s) => s.init);
  const error = useStore((s) => s.error);
  const setError = useStore((s) => s.setError);
  const notice = useStore((s) => s.notice);
  const setNotice = useStore((s) => s.setNotice);
  const refreshNotifications = useStore((s) => s.refreshNotifications);
  const applyFlightUpdate = useStore((s) => s.applyFlightUpdate);
  const applyFlightDelete = useStore((s) => s.applyFlightDelete);
  const applyNotificationsUpdate = useStore((s) => s.applyNotificationsUpdate);
  const users = useStore((s) => s.users);
  const showAll = useStore((s) => s.showAll);
  const { mode } = useThemeMode();
  const theme = useMemo(() => createAppTheme(mode), [mode]);
  const processedTokenRef = useRef<string | null>(null);

  useEffect(() => {
    void init();
  }, [init]);

  useEffect(() => {
    if (auth !== 'authenticated') return;
    return connectSSE(
      {
        onFlight: (f) => applyFlightUpdate(f),
        onDelete: (id) => applyFlightDelete(id),
        onNotifications: (n) => applyNotificationsUpdate(n),
      },
      { showAll },
    );
  }, [auth, applyFlightUpdate, applyFlightDelete, applyNotificationsUpdate, showAll]);

  useEffect(() => {
    if (auth !== 'authenticated') return;
    const params = new URLSearchParams(window.location.search);
    let token = params.get('friend_accept');
    let fromStash = false;
    if (!token) {
      try {
        token = window.sessionStorage.getItem('aerly.pending_friend_accept');
        if (token) fromStash = true;
      } catch {
        token = null;
      }
    }
    if (!token) return;
    if (processedTokenRef.current === token) return;
    processedTokenRef.current = token;
    void (async () => {
      try {
        const r = await api.acceptFriendToken(token);
        if (r.already) {
          setNotice({
            message: "You're already friends — nothing to accept.",
            severity: 'info',
          });
        } else {
          const friend = r.friendship
            ? users.find((u) => u.id === r.friendship!.friend_id)
            : undefined;
          const label = friend?.name?.trim() || 'them';
          setNotice({
            message: `You're now friends with ${label}.`,
            severity: 'success',
          });
        }
        void refreshNotifications();
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        params.delete('friend_accept');
        const qs = params.toString();
        const url =
          window.location.pathname + (qs ? '?' + qs : '') + window.location.hash;
        window.history.replaceState({}, '', url);
        if (fromStash) {
          try {
            window.sessionStorage.removeItem('aerly.pending_friend_accept');
          } catch {
            /* ignore */
          }
        }
      }
    })();
  }, [auth, users, refreshNotifications, setError, setNotice]);

  // window.location.pathname is safe here because /privacy and /terms are only
  // reached via full page loads — there is no client-side pushState navigation.
  let body;
  if (window.location.pathname === '/privacy') {
    body = <PrivacyPolicy />;
  } else if (window.location.pathname === '/terms') {
    body = <TermsOfService />;
  } else if (auth === 'loading') {
    body = (
      <Box sx={{ display: 'grid', placeItems: 'center', minHeight: '100vh' }}>
        <CircularProgress />
      </Box>
    );
  } else if (auth === 'anonymous') {
    body = <Login />;
  } else {
    body = <AppShell />;
  }

  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <LocalizationProvider dateAdapter={AdapterDateFns}>
        {body}
        <Snackbar
          open={error !== null}
          autoHideDuration={6000}
          onClose={() => setError(null)}
          anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
        >
          <Alert severity="error" variant="filled" onClose={() => setError(null)}>
            {error}
          </Alert>
        </Snackbar>
        <Snackbar
          open={notice !== null}
          autoHideDuration={6000}
          onClose={() => setNotice(null)}
          anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
        >
          {notice ? (
            <Alert severity={notice.severity} variant="filled" onClose={() => setNotice(null)}>
              {notice.message}
            </Alert>
          ) : undefined}
        </Snackbar>
      </LocalizationProvider>
    </ThemeProvider>
  );
}

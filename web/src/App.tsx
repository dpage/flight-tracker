import { useEffect, useMemo, useRef } from 'react';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { Alert, Box, CircularProgress, CssBaseline, Snackbar, ThemeProvider } from '@mui/material';
import { LocalizationProvider } from '@mui/x-date-pickers/LocalizationProvider';
import { AdapterDateFns } from '@mui/x-date-pickers/AdapterDateFnsV3';

import { useStore } from './state/store';
import { connectSSE } from './sse';
import { api } from './api/client';
import { createAppTheme, useThemeMode } from './theme';
import Login from './components/Login';
import Layout from './components/Layout';
import PrivacyPolicy from './components/PrivacyPolicy';
import TermsOfService from './components/TermsOfService';
import TripList from './pages/TripList';
import TripDetail from './pages/TripDetail';
import TripTimeline from './pages/TripTimeline';
import TripMap from './pages/TripMap';
import Tracker from './pages/Tracker';

export default function App() {
  const auth = useStore((s) => s.auth);
  const init = useStore((s) => s.init);
  const error = useStore((s) => s.error);
  const setError = useStore((s) => s.setError);
  const notice = useStore((s) => s.notice);
  const setNotice = useStore((s) => s.setNotice);
  const refreshNotifications = useStore((s) => s.refreshNotifications);
  const refreshFriendships = useStore((s) => s.refreshFriendships);
  const refreshUsers = useStore((s) => s.refreshUsers);
  const applyPlanPartUpdate = useStore((s) => s.applyPlanPartUpdate);
  const loadTrip = useStore((s) => s.loadTrip);
  const loadTracker = useStore((s) => s.loadTracker);
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
        // The poller broadcasts plan_part.updated (a TrackerPartDTO) when a
        // tracked part refreshes. Fold it into the open trip's timeline and the
        // tracker convergence list so the shared timeline updates live.
        onPlanPart: (part) => applyPlanPartUpdate(part),
        // trip.updated / plan.updated aren't emitted by the backend yet (see
        // sse.ts). These handlers are wired defensively so the moment the
        // backend starts publishing them the relevant view refetches itself;
        // until then they never fire.
        onTrip: (id) => {
          const cur = useStore.getState().currentTrip;
          if (cur && cur.id === id) void loadTrip(id);
        },
        onPlan: (tripId) => {
          const cur = useStore.getState().currentTrip;
          if (cur && cur.id === tripId) void loadTrip(tripId);
          void loadTracker();
        },
        onNotifications: (n) => {
          applyNotificationsUpdate(n);
          // The server fires notifications.updated whenever the viewer's
          // friendship state changes (incoming invite, peer accepts/declines,
          // viewer cancels, etc.). The badge count is one consequence; the
          // friend list and the cached user records need to come along too,
          // or the share/passenger pickers and the friends dialog will keep
          // showing stale "User #N" entries for newly-accepted friends.
          void refreshFriendships();
          void refreshUsers();
        },
      },
      { showAll },
    );
  }, [
    auth,
    applyPlanPartUpdate,
    loadTrip,
    loadTracker,
    applyNotificationsUpdate,
    refreshFriendships,
    refreshUsers,
    showAll,
  ]);

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
        const url = window.location.pathname + (qs ? '?' + qs : '') + window.location.hash;
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

  // /privacy and /terms render regardless of auth (they're linked from the
  // login page and from emails). Everything else is gated: a spinner while
  // auth is resolving, the Login screen when anonymous, and the routed app
  // chrome once authenticated. The home route (`/`) is the trip list.
  let gated;
  if (auth === 'loading') {
    gated = (
      <Box sx={{ display: 'grid', placeItems: 'center', minHeight: '100vh' }}>
        <CircularProgress />
      </Box>
    );
  } else if (auth === 'anonymous') {
    gated = <Login />;
  } else {
    gated = (
      <Routes>
        <Route element={<Layout />}>
          <Route index element={<TripList />} />
          <Route path="trips/:id" element={<TripDetail />}>
            <Route index element={<TripTimeline />} />
            <Route path="map" element={<TripMap />} />
          </Route>
          <Route path="tracker" element={<Tracker />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    );
  }

  const body = (
    <Routes>
      <Route path="/privacy" element={<PrivacyPolicy />} />
      <Route path="/terms" element={<TermsOfService />} />
      <Route path="*" element={gated} />
    </Routes>
  );

  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <LocalizationProvider dateAdapter={AdapterDateFns}>
        <BrowserRouter>{body}</BrowserRouter>
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

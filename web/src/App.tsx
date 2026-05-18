import { useEffect } from 'react';
import { Alert, Box, CircularProgress, Snackbar } from '@mui/material';

import { useStore } from './state/store';
import { connectSSE } from './sse';
import Login from './components/Login';
import AppShell from './components/AppShell';

export default function App() {
  const auth = useStore((s) => s.auth);
  const init = useStore((s) => s.init);
  const error = useStore((s) => s.error);
  const setError = useStore((s) => s.setError);
  const applyFlightUpdate = useStore((s) => s.applyFlightUpdate);

  useEffect(() => {
    void init();
  }, [init]);

  useEffect(() => {
    if (auth !== 'authenticated') return;
    return connectSSE((f) => applyFlightUpdate(f));
  }, [auth, applyFlightUpdate]);

  let body;
  if (auth === 'loading') {
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
    <>
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
    </>
  );
}

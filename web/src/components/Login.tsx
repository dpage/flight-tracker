import { useEffect, useState } from 'react';
import {
  Box,
  Button,
  Divider,
  Paper,
  Stack,
  TextField,
  Typography,
} from '@mui/material';
import GitHubIcon from '@mui/icons-material/GitHub';
import GoogleIcon from '@mui/icons-material/Google';
import LoginIcon from '@mui/icons-material/Login';
import FlightTakeoffIcon from '@mui/icons-material/FlightTakeoff';

import { api } from '../api/client';
import type { AuthProvider } from '../api/types';

// Per-provider icon mapping. Unknown providers (or future additions) get a
// generic icon rather than rendering an empty space.
function iconFor(name: string) {
  switch (name) {
    case 'github':
      return <GitHubIcon />;
    case 'google':
      return <GoogleIcon />;
    default:
      return <LoginIcon />;
  }
}

export default function Login() {
  // null until the /auth/providers request resolves; an empty array means
  // "the backend reports no configured providers" (different from "we
  // haven't asked yet"). This split avoids the first-paint flash where
  // the page briefly looked like there were no sign-in methods.
  const [providers, setProviders] = useState<AuthProvider[] | null>(null);
  const [devBypass, setDevBypass] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void api.getAuthProviders().then((ps) => {
      if (!cancelled) setProviders(ps);
    });
    void api.getDevAuthBypassEnabled().then((enabled) => {
      if (!cancelled) setDevBypass(enabled);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <Box
      sx={{
        minHeight: '100vh',
        display: 'grid',
        placeItems: 'center',
        bgcolor: 'background.default',
        p: 2,
      }}
    >
      <Paper sx={{ p: 4, maxWidth: 420, width: '100%' }} elevation={3}>
        <Stack spacing={3} alignItems="center" textAlign="center">
          <FlightTakeoffIcon color="primary" sx={{ fontSize: 56 }} />
          <Typography variant="h4">Aerly</Typography>
          <Typography variant="body1" color="text.secondary">
            Track your friends&rsquo; flights to PostgreSQL conferences.
          </Typography>
          <Stack spacing={1.5} sx={{ alignSelf: 'stretch' }}>
            {providers === null ? (
              <Button variant="contained" size="large" disabled>
                Loading sign-in options…
              </Button>
            ) : (
              providers.map((p) => (
                <Button
                  key={p.name}
                  variant="contained"
                  size="large"
                  startIcon={iconFor(p.name)}
                  href={`/auth/${p.name}/login`}
                >
                  Sign in with {p.label}
                </Button>
              ))
            )}
          </Stack>
          <Typography variant="caption" color="text.secondary">
            Access is restricted to invited users.
          </Typography>
          {devBypass && (
            <>
              <Divider flexItem>DEV</Divider>
              {/* Plain GET form: the browser navigates to
                  /auth/dev-login?login=<value>, the server sets the session
                  cookie and 302s back to /. */}
              <Stack
                component="form"
                action="/auth/dev-login"
                method="GET"
                spacing={1.5}
                sx={{ alignSelf: 'stretch' }}
              >
                <TextField
                  name="login"
                  label="Username"
                  size="small"
                  required
                  autoComplete="off"
                  inputProps={{ 'aria-label': 'dev login username' }}
                />
                <Button type="submit" variant="outlined">
                  Sign in as dev user
                </Button>
                <Typography variant="caption" color="text.secondary">
                  DEV_AUTH_BYPASS is enabled. Do not use in production.
                </Typography>
              </Stack>
            </>
          )}
        </Stack>
      </Paper>
    </Box>
  );
}

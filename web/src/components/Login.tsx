import { Box, Button, Paper, Stack, Typography } from '@mui/material';
import GitHubIcon from '@mui/icons-material/GitHub';
import FlightTakeoffIcon from '@mui/icons-material/FlightTakeoff';

export default function Login() {
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
          <Typography variant="h4">Flight Tracker</Typography>
          <Typography variant="body1" color="text.secondary">
            Track your friends&rsquo; flights to PostgreSQL conferences.
          </Typography>
          <Button
            variant="contained"
            size="large"
            startIcon={<GitHubIcon />}
            href="/auth/github/login"
            sx={{ alignSelf: 'stretch' }}
          >
            Sign in with GitHub
          </Button>
          <Typography variant="caption" color="text.secondary">
            Access is restricted to invited users.
          </Typography>
        </Stack>
      </Paper>
    </Box>
  );
}

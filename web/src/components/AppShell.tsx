import { useState } from 'react';
import {
  AppBar,
  Avatar,
  Box,
  Button,
  IconButton,
  Stack,
  Toolbar,
  Tooltip,
  Typography,
} from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import LogoutIcon from '@mui/icons-material/Logout';
import AdminPanelSettingsIcon from '@mui/icons-material/AdminPanelSettings';
import FlightTakeoffIcon from '@mui/icons-material/FlightTakeoff';

import { useStore } from '../state/store';
import FlightList from './FlightList';
import FlightMap from './FlightMap';
import FlightDialog from './FlightDialog';
import AdminDialog from './AdminDialog';

export default function AppShell() {
  const me = useStore((s) => s.me);
  const logout = useStore((s) => s.logout);
  const [flightDialog, setFlightDialog] = useState<{ open: boolean; editId: number | null }>({
    open: false,
    editId: null,
  });
  const [adminOpen, setAdminOpen] = useState(false);

  return (
    <Box sx={{ height: '100vh', display: 'flex', flexDirection: 'column' }}>
      <AppBar position="static" color="default" elevation={1}>
        <Toolbar variant="dense">
          <FlightTakeoffIcon color="primary" sx={{ mr: 1 }} />
          <Typography variant="h6" sx={{ flexGrow: 1 }}>
            Flight Tracker
          </Typography>
          <Button
            startIcon={<AddIcon />}
            onClick={() => setFlightDialog({ open: true, editId: null })}
            size="small"
            sx={{ mr: 1 }}
          >
            Add flight
          </Button>
          {me?.is_superuser && (
            <Tooltip title="Manage users">
              <IconButton size="small" onClick={() => setAdminOpen(true)} sx={{ mr: 1 }}>
                <AdminPanelSettingsIcon />
              </IconButton>
            </Tooltip>
          )}
          <Tooltip title={me ? `${me.github_login} — sign out` : 'Sign out'}>
            <Stack direction="row" alignItems="center" spacing={1}>
              <Avatar src={me?.avatar_url} sx={{ width: 28, height: 28 }}>
                {me?.github_login.charAt(0).toUpperCase()}
              </Avatar>
              <IconButton size="small" onClick={() => void logout()}>
                <LogoutIcon fontSize="small" />
              </IconButton>
            </Stack>
          </Tooltip>
        </Toolbar>
      </AppBar>

      <Box sx={{ display: 'flex', flexGrow: 1, minHeight: 0 }}>
        <Box
          sx={{
            width: 360,
            borderRight: 1,
            borderColor: 'divider',
            overflowY: 'auto',
            bgcolor: 'background.paper',
          }}
        >
          <FlightList onEditFlight={(id) => setFlightDialog({ open: true, editId: id })} />
        </Box>
        <Box sx={{ flexGrow: 1, position: 'relative' }}>
          <FlightMap />
        </Box>
      </Box>

      <FlightDialog
        open={flightDialog.open}
        editId={flightDialog.editId}
        onClose={() => setFlightDialog({ open: false, editId: null })}
      />
      <AdminDialog open={adminOpen} onClose={() => setAdminOpen(false)} />
    </Box>
  );
}

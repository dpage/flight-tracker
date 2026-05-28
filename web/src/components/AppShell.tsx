import { useEffect, useState } from 'react';
import {
  AppBar,
  Avatar,
  Box,
  Button,
  Divider,
  IconButton,
  ListItemIcon,
  Menu,
  MenuItem,
  Toolbar,
  Tooltip,
  Typography,
  useMediaQuery,
  useTheme,
} from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import AdminPanelSettingsIcon from '@mui/icons-material/AdminPanelSettings';
import BarChartIcon from '@mui/icons-material/BarChart';
import ChevronLeftIcon from '@mui/icons-material/ChevronLeft';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import DarkModeIcon from '@mui/icons-material/DarkMode';
import EmailIcon from '@mui/icons-material/EmailOutlined';
import FlightTakeoffIcon from '@mui/icons-material/FlightTakeoff';
import LightModeIcon from '@mui/icons-material/LightMode';
import LogoutIcon from '@mui/icons-material/Logout';
import SettingsBrightnessIcon from '@mui/icons-material/SettingsBrightness';

import { useStore } from '../state/store';
import { useThemeMode, type ThemePreference } from '../theme';
import FlightList from './FlightList';
import FlightMap from './FlightMap';
import FlightDialog from './FlightDialog';
import AdminDialog from './AdminDialog';
import EmailsDialog from './EmailsDialog';
import StatsDialog from './StatsDialog';

export default function AppShell() {
  const me = useStore((s) => s.me);
  const logout = useStore((s) => s.logout);
  const capabilities = useStore((s) => s.capabilities);
  const { preference: themePreference, setPreference: setThemePreference } = useThemeMode();
  const theme = useTheme();
  const isNarrow = useMediaQuery(theme.breakpoints.down('sm'));
  const [flightDialog, setFlightDialog] = useState<{ open: boolean; editId: number | null }>({
    open: false,
    editId: null,
  });
  const [adminOpen, setAdminOpen] = useState(false);
  const [emailsOpen, setEmailsOpen] = useState(false);
  const [statsOpen, setStatsOpen] = useState(false);
  const [menuAnchor, setMenuAnchor] = useState<HTMLElement | null>(null);
  const [sidebarOpen, setSidebarOpen] = useState(() => !isNarrow);

  useEffect(() => {
    const t = window.setTimeout(() => window.dispatchEvent(new Event('resize')), 220);
    return () => window.clearTimeout(t);
  }, [sidebarOpen]);

  const closeMenu = () => setMenuAnchor(null);

  return (
    <Box sx={{ height: '100vh', display: 'flex', flexDirection: 'column' }}>
      <AppBar position="static" color="default" elevation={1}>
        <Toolbar variant="dense">
          <FlightTakeoffIcon color="primary" sx={{ mr: 1 }} />
          <Typography variant="h6" sx={{ flexGrow: 1 }}>
            Aerly
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
          <Tooltip title="Account menu">
            <IconButton
              size="small"
              onClick={(e) => setMenuAnchor(e.currentTarget)}
              aria-label="Account menu"
            >
              <Avatar src={me?.avatar_url} sx={{ width: 28, height: 28 }}>
                {me?.username.charAt(0).toUpperCase()}
              </Avatar>
            </IconButton>
          </Tooltip>
          <Menu
            anchorEl={menuAnchor}
            open={menuAnchor !== null}
            onClose={closeMenu}
            anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
            transformOrigin={{ vertical: 'top', horizontal: 'right' }}
          >
            {me && (
              <MenuItem disabled sx={{ opacity: '1 !important' }}>
                <Typography variant="caption" color="text.secondary">
                  Signed in as {me.username}
                </Typography>
              </MenuItem>
            )}
            <Divider />
            {capabilities.email_ingest_enabled && (
              <MenuItem
                onClick={() => {
                  closeMenu();
                  setEmailsOpen(true);
                }}
              >
                <ListItemIcon>
                  <EmailIcon fontSize="small" />
                </ListItemIcon>
                Email addresses…
              </MenuItem>
            )}
            <MenuItem
              onClick={() => {
                closeMenu();
                setStatsOpen(true);
              }}
            >
              <ListItemIcon>
                <BarChartIcon fontSize="small" />
              </ListItemIcon>
              Statistics…
            </MenuItem>
            <Divider />
            <MenuItem disabled sx={{ opacity: '1 !important' }}>
              <Typography variant="caption" color="text.secondary">
                Appearance
              </Typography>
            </MenuItem>
            {(
              [
                { value: 'light', label: 'Light', Icon: LightModeIcon },
                { value: 'dark', label: 'Dark', Icon: DarkModeIcon },
                { value: 'system', label: 'System', Icon: SettingsBrightnessIcon },
              ] as const
            ).map(({ value, label, Icon }) => (
              <MenuItem
                key={value}
                selected={themePreference === value}
                onClick={() => {
                  setThemePreference(value as ThemePreference);
                  closeMenu();
                }}
              >
                <ListItemIcon>
                  <Icon fontSize="small" />
                </ListItemIcon>
                {label}
              </MenuItem>
            ))}
            <Divider />
            <MenuItem
              onClick={() => {
                closeMenu();
                void logout();
              }}
            >
              <ListItemIcon>
                <LogoutIcon fontSize="small" />
              </ListItemIcon>
              Sign out
            </MenuItem>
          </Menu>
        </Toolbar>
      </AppBar>

      <Box sx={{ display: 'flex', flexGrow: 1, minHeight: 0 }}>
        <Box
          sx={{
            width: sidebarOpen ? { xs: '85vw', sm: 360 } : 0,
            minWidth: sidebarOpen ? { xs: '85vw', sm: 360 } : 0,
            transition: 'width 200ms ease, min-width 200ms ease',
            borderRight: sidebarOpen ? 1 : 0,
            borderColor: 'divider',
            overflowY: 'auto',
            overflowX: 'hidden',
            bgcolor: 'background.paper',
          }}
        >
          <FlightList onEditFlight={(id) => setFlightDialog({ open: true, editId: id })} />
        </Box>
        <Box sx={{ flexGrow: 1, position: 'relative', minWidth: 0 }}>
          <FlightMap />
          <Tooltip title={sidebarOpen ? 'Hide flights' : 'Show flights'} placement="right">
            <IconButton
              onClick={() => setSidebarOpen((o) => !o)}
              size="small"
              aria-label={sidebarOpen ? 'Hide flight list' : 'Show flight list'}
              sx={{
                position: 'absolute',
                top: '50%',
                left: 0,
                transform: 'translateY(-50%)',
                bgcolor: 'background.paper',
                color: 'text.primary',
                border: 1,
                borderColor: 'divider',
                borderLeft: 0,
                borderRadius: '0 8px 8px 0',
                boxShadow: 2,
                px: 0.25,
                py: 1.5,
                zIndex: 2,
                '&:hover': { bgcolor: 'background.paper' },
              }}
            >
              {sidebarOpen ? (
                <ChevronLeftIcon fontSize="small" />
              ) : (
                <ChevronRightIcon fontSize="small" />
              )}
            </IconButton>
          </Tooltip>
        </Box>
      </Box>

      <FlightDialog
        open={flightDialog.open}
        editId={flightDialog.editId}
        onClose={() => setFlightDialog({ open: false, editId: null })}
      />
      <AdminDialog open={adminOpen} onClose={() => setAdminOpen(false)} />
      <EmailsDialog open={emailsOpen} onClose={() => setEmailsOpen(false)} />
      <StatsDialog open={statsOpen} onClose={() => setStatsOpen(false)} />
    </Box>
  );
}

import { useState } from 'react';
import { Link as RouterLink, Outlet, useLocation } from 'react-router-dom';
import {
  AppBar,
  Avatar,
  Badge,
  Box,
  Button,
  Chip,
  Divider,
  IconButton,
  ListItemIcon,
  Menu,
  MenuItem,
  Toolbar,
  Tooltip,
  Typography,
} from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import AdminPanelSettingsIcon from '@mui/icons-material/AdminPanelSettings';
import BarChartIcon from '@mui/icons-material/BarChart';
import CalendarMonthIcon from '@mui/icons-material/CalendarMonth';
import DarkModeIcon from '@mui/icons-material/DarkMode';
import EmailIcon from '@mui/icons-material/EmailOutlined';
import FlightTakeoffIcon from '@mui/icons-material/FlightTakeoff';
import LightModeIcon from '@mui/icons-material/LightMode';
import LogoutIcon from '@mui/icons-material/Logout';
import NotificationsIcon from '@mui/icons-material/NotificationsOutlined';
import PeopleIcon from '@mui/icons-material/PeopleOutline';
import SettingsBrightnessIcon from '@mui/icons-material/SettingsBrightness';

import { useStore } from '../state/store';
import { userInitial, userName } from '../lib/format';
import { useThemeMode, type ThemePreference } from '../theme';
import AddToTripDialog from './AddToTripDialog';
import AdminDialog from './AdminDialog';
import AlertPrefsDialog from './AlertPrefsDialog';
import EmailsDialog from './EmailsDialog';
import FriendsDialog from './FriendsDialog';
import StatsDialog from './StatsDialog';
import CalendarSubscribeDialog from './CalendarSubscribeDialog';

/** The authenticated app chrome for the trip-planning redesign (spec §11).
 *
 * Holds the top bar (Trips / Tracker nav, the "New trip"/"Add to trip" primary
 * action that replaces the old "Add flight", and the account menu) plus the
 * account-level dialogs, and renders the routed page via `<Outlet>`. The legacy
 * flight-centric `AppShell` stays reachable at `/flights` until Wave 3 removes
 * it. Dialogs live below routing, exactly as before. */
export default function Layout() {
  const me = useStore((s) => s.me);
  const logout = useStore((s) => s.logout);
  const capabilities = useStore((s) => s.capabilities);
  const pendingRequests = useStore((s) => s.notifications.friend_requests_pending);
  const { preference: themePreference, setPreference: setThemePreference } = useThemeMode();
  const location = useLocation();

  const [addOpen, setAddOpen] = useState(false);
  const [adminOpen, setAdminOpen] = useState(false);
  const [emailsOpen, setEmailsOpen] = useState(false);
  const [friendsOpen, setFriendsOpen] = useState(false);
  const [statsOpen, setStatsOpen] = useState(false);
  const [alertPrefsOpen, setAlertPrefsOpen] = useState(false);
  const [subscribeOpen, setSubscribeOpen] = useState(false);
  const [menuAnchor, setMenuAnchor] = useState<HTMLElement | null>(null);

  const closeMenu = () => setMenuAnchor(null);
  const onTracker = location.pathname.startsWith('/tracker');

  return (
    <Box sx={{ height: '100vh', display: 'flex', flexDirection: 'column' }}>
      <AppBar position="static" color="default" elevation={1}>
        <Toolbar variant="dense">
          <FlightTakeoffIcon color="primary" sx={{ mr: 1 }} />
          <Typography
            variant="h6"
            component={RouterLink}
            to="/"
            sx={{ flexGrow: 0, mr: 3, color: 'inherit', textDecoration: 'none' }}
          >
            Aerly
          </Typography>
          <Button
            component={RouterLink}
            to="/"
            size="small"
            color={onTracker ? 'inherit' : 'primary'}
          >
            Trips
          </Button>
          <Button
            component={RouterLink}
            to="/tracker"
            size="small"
            color={onTracker ? 'primary' : 'inherit'}
            sx={{ mr: 1 }}
          >
            Tracker
          </Button>
          <Box sx={{ flexGrow: 1 }} />
          <Button
            startIcon={<AddIcon />}
            onClick={() => setAddOpen(true)}
            size="small"
            sx={{ mr: 1 }}
          >
            Add to trip
          </Button>
          {me?.is_superuser && (
            <Tooltip title="Manage users">
              <IconButton size="small" onClick={() => setAdminOpen(true)} sx={{ mr: 1 }}>
                <AdminPanelSettingsIcon />
              </IconButton>
            </Tooltip>
          )}
          <Badge
            badgeContent={pendingRequests}
            color="error"
            overlap="circular"
            invisible={pendingRequests === 0}
            anchorOrigin={{ vertical: 'top', horizontal: 'right' }}
          >
            <Tooltip title="Account menu">
              <IconButton
                size="small"
                onClick={(e) => setMenuAnchor(e.currentTarget)}
                aria-label="Account menu"
              >
                <Avatar src={me?.avatar_url} sx={{ width: 28, height: 28 }}>
                  {me && userInitial(me)}
                </Avatar>
              </IconButton>
            </Tooltip>
          </Badge>
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
                  Signed in as {userName(me)}
                </Typography>
              </MenuItem>
            )}
            <Divider />
            <MenuItem
              onClick={() => {
                closeMenu();
                setFriendsOpen(true);
              }}
            >
              <ListItemIcon>
                <PeopleIcon fontSize="small" />
              </ListItemIcon>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexGrow: 1 }}>
                <Box>Friends…</Box>
                {pendingRequests > 0 && (
                  <Chip label={pendingRequests} size="small" color="error" sx={{ ml: 'auto' }} />
                )}
              </Box>
            </MenuItem>
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
            <MenuItem
              onClick={() => {
                closeMenu();
                setAlertPrefsOpen(true);
              }}
            >
              <ListItemIcon>
                <NotificationsIcon fontSize="small" />
              </ListItemIcon>
              Alert preferences…
            </MenuItem>
            <MenuItem
              onClick={() => {
                closeMenu();
                setSubscribeOpen(true);
              }}
            >
              <ListItemIcon>
                <CalendarMonthIcon fontSize="small" />
              </ListItemIcon>
              Subscribe to calendar…
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

      <Box sx={{ flexGrow: 1, minHeight: 0, overflowY: 'auto' }}>
        <Outlet />
      </Box>

      <AddToTripDialog open={addOpen} tripId={null} onClose={() => setAddOpen(false)} />
      <AdminDialog open={adminOpen} onClose={() => setAdminOpen(false)} />
      <EmailsDialog open={emailsOpen} onClose={() => setEmailsOpen(false)} />
      <FriendsDialog open={friendsOpen} onClose={() => setFriendsOpen(false)} />
      <StatsDialog open={statsOpen} onClose={() => setStatsOpen(false)} />
      <AlertPrefsDialog open={alertPrefsOpen} onClose={() => setAlertPrefsOpen(false)} />
      <CalendarSubscribeDialog
        open={subscribeOpen}
        onClose={() => setSubscribeOpen(false)}
        scope="me"
      />
    </Box>
  );
}

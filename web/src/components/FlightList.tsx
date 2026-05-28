import { Fragment, useEffect, useState } from 'react';
import {
  Avatar,
  AvatarGroup,
  Box,
  Chip,
  FormControlLabel,
  IconButton,
  Stack,
  Switch,
  Tooltip,
  Typography,
} from '@mui/material';
import PublicIcon from '@mui/icons-material/Public';
import EditIcon from '@mui/icons-material/Edit';
import DeleteIcon from '@mui/icons-material/DeleteOutline';

import { useStore } from '../state/store';
import { useVisibleFlights } from '../state/visibleFlights';
import type { Flight, FlightStatus, User } from '../api/types';
import { fmtDateTime, fmtRelative } from '../lib/format';
import FlightDetailPanel from './FlightDetailPanel';

interface Props {
  onEditFlight: (id: number) => void;
}

export default function FlightList({ onEditFlight }: Props) {
  const flights = useVisibleFlights();
  const users = useStore((s) => s.users);
  const me = useStore((s) => s.me);
  const selectedFlightId = useStore((s) => s.selectedFlightId);
  const selectFlight = useStore((s) => s.selectFlight);
  const deleteFlight = useStore((s) => s.deleteFlight);
  const showAll = useStore((s) => s.showAll);
  const setShowAll = useStore((s) => s.setShowAll);
  const showOld = useStore((s) => s.showOld);
  const setShowOld = useStore((s) => s.setShowOld);
  const showMineOnly = useStore((s) => s.showMineOnly);
  const setShowMineOnly = useStore((s) => s.setShowMineOnly);

  const usersById = new Map(users.map((u) => [u.id, u]));

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', minHeight: '100%' }}>
      <Box
        sx={{
          px: 2,
          py: 0.5,
          borderBottom: 1,
          borderColor: 'divider',
          bgcolor: 'background.default',
          display: 'flex',
          alignItems: 'center',
          gap: 2,
          flexWrap: 'wrap',
        }}
      >
        <Tooltip title="Hide flights you didn't create and aren't a passenger on.">
          <FormControlLabel
            control={
              <Switch
                checked={showMineOnly}
                onChange={(e) => setShowMineOnly(e.target.checked)}
                size="small"
              />
            }
            label={
              <Typography variant="caption" color="text.secondary">
                Only my flights
              </Typography>
            }
          />
        </Tooltip>
        <Tooltip title="Include flights that landed more than 24 hours ago.">
          <FormControlLabel
            control={
              <Switch
                checked={showOld}
                onChange={(e) => void setShowOld(e.target.checked)}
                size="small"
              />
            }
            label={
              <Typography variant="caption" color="text.secondary">
                Show old flights
              </Typography>
            }
          />
        </Tooltip>
        {me?.is_superuser && (
          <Tooltip title="Superuser-only: include every flight in the list, not just yours and ones shared with you.">
            <FormControlLabel
              control={
                <Switch
                  checked={showAll}
                  onChange={(e) => void setShowAll(e.target.checked)}
                  size="small"
                />
              }
              label={
                <Typography variant="caption" color="text.secondary">
                  Show all flights
                </Typography>
              }
            />
          </Tooltip>
        )}
      </Box>
      <Box sx={{ flexGrow: 1 }}>
        {flights.length === 0 ? (
          <Box sx={{ p: 3 }}>
            <Typography color="text.secondary" variant="body2">
              No flights yet. Click <strong>Add flight</strong> in the top bar to track your first
              journey.
            </Typography>
          </Box>
        ) : (
          <Stack divider={<Box sx={{ borderBottom: 1, borderColor: 'divider' }} />}>
            {flights.map((f) => {
              const selected = f.id === selectedFlightId;
              const passengers = f.passenger_ids
                .map((id) => usersById.get(id))
                .filter((u): u is User => u !== undefined);
              const sharedWith = (f.shared_user_ids ?? [])
                .map((id) => usersById.get(id))
                .filter((u): u is User => u !== undefined);
              const owner = f.created_by != null ? usersById.get(f.created_by) : undefined;
              return (
                <Fragment key={f.id}>
                  <FlightRow
                    flight={f}
                    passengers={passengers}
                    owner={owner}
                    selected={selected}
                    onSelect={() => selectFlight(selected ? null : f.id)}
                    onEdit={() => onEditFlight(f.id)}
                    onDelete={() => {
                      if (confirm(`Delete flight ${f.ident}?`)) void deleteFlight(f.id);
                    }}
                  />
                  {selected && (
                    <FlightDetailPanel
                      flight={f}
                      passengers={passengers}
                      sharedWith={sharedWith}
                      owner={owner}
                    />
                  )}
                </Fragment>
              );
            })}
          </Stack>
        )}
      </Box>
      <PollFooter />
    </Box>
  );
}

// PollFooter shows a tiny "last update Xs ago · next ~Ys" line at the
// bottom of the flight list. The "next" countdown is best-effort — it
// assumes the server's POLL_INTERVAL plus a small jitter; if no event
// has arrived yet we just show "awaiting first update".
function PollFooter() {
  const lastUpdateAt = useStore((s) => s.lastUpdateAt);
  const pollIntervalSec = useStore((s) => s.capabilities.poll_interval_sec);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, []);

  let body: React.ReactNode;
  if (!lastUpdateAt) {
    body = `Awaiting first update — polls every ${pollIntervalSec}s`;
  } else {
    const sinceSec = Math.max(0, Math.floor((now - lastUpdateAt) / 1000));
    const remainSec = Math.max(0, pollIntervalSec - sinceSec);
    body = (
      <>
        Last update {fmtRelative(sinceSec)} ago · next ~{remainSec}s
      </>
    );
  }

  return (
    <Box
      sx={{
        px: 2,
        py: 1,
        borderTop: 1,
        borderColor: 'divider',
        bgcolor: 'background.default',
        position: 'sticky',
        bottom: 0,
      }}
    >
      <Typography variant="caption" color="text.secondary">
        {body}
      </Typography>
    </Box>
  );
}

interface FlightRowProps {
  flight: Flight;
  passengers: User[];
  owner: User | undefined;
  selected: boolean;
  onSelect: () => void;
  onEdit: () => void;
  onDelete: () => void;
}

function FlightRow({
  flight,
  passengers,
  owner,
  selected,
  onSelect,
  onEdit,
  onDelete,
}: FlightRowProps) {
  const eta = flight.estimated_in ?? flight.scheduled_in;
  const missingCoords =
    flight.origin_lat == null ||
    flight.origin_lon == null ||
    flight.dest_lat == null ||
    flight.dest_lon == null;
  return (
    <Box
      onClick={onSelect}
      sx={{
        px: 2,
        py: 1.5,
        cursor: 'pointer',
        bgcolor: selected ? 'action.selected' : 'transparent',
        '&:hover': { bgcolor: 'action.hover' },
      }}
    >
      <Stack direction="row" alignItems="center" spacing={1}>
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Stack direction="row" alignItems="center" spacing={1}>
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
              {flight.ident}
            </Typography>
            <StatusChip status={flight.status} />
            {flight.is_public && (
              <Tooltip title="Public flight — visible to every authenticated user">
                <PublicIcon fontSize="small" sx={{ color: 'text.secondary' }} />
              </Tooltip>
            )}
            {owner && (
              <Tooltip title={`Added by ${owner.username}`}>
                <Stack direction="row" alignItems="center" spacing={0.5} sx={{ ml: 'auto' }}>
                  <Avatar
                    src={owner.avatar_url}
                    sx={{ width: 18, height: 18, fontSize: 10 }}
                  >
                    {owner.username.charAt(0).toUpperCase()}
                  </Avatar>
                  <Typography variant="caption" color="text.secondary" noWrap>
                    {owner.username}
                  </Typography>
                </Stack>
              </Tooltip>
            )}
          </Stack>
          <Stack direction="row" alignItems="center" spacing={0.75}>
            <Typography variant="body2" color="text.secondary" noWrap>
              {flight.origin_iata || '???'} → {flight.dest_iata || '???'}
            </Typography>
            {missingCoords && (
              <Tooltip title="Origin/destination IATA codes missing or unknown — flight won't appear on the map. Edit the flight to fix.">
                <Chip
                  label="no map"
                  size="small"
                  color="warning"
                  variant="outlined"
                  sx={{ height: 18, fontSize: 10 }}
                />
              </Tooltip>
            )}
          </Stack>
          <Typography variant="caption" color="text.secondary">
            {fmtDateTime(flight.scheduled_out, flight.origin_tz)} →{' '}
            {fmtDateTime(eta, flight.dest_tz)}
          </Typography>
          {passengers.length > 0 && (
            <AvatarGroup
              max={6}
              sx={{ mt: 0.5, justifyContent: 'flex-start', '& .MuiAvatar-root': { width: 24, height: 24, fontSize: 12 } }}
            >
              {passengers.map((u) => (
                <Tooltip key={u.id} title={u.username}>
                  <Avatar src={u.avatar_url}>{u.username.charAt(0).toUpperCase()}</Avatar>
                </Tooltip>
              ))}
            </AvatarGroup>
          )}
        </Box>
        <Stack direction="row" spacing={0.5}>
          <IconButton
            size="small"
            onClick={(e) => {
              e.stopPropagation();
              onEdit();
            }}
          >
            <EditIcon fontSize="small" />
          </IconButton>
          <IconButton
            size="small"
            onClick={(e) => {
              e.stopPropagation();
              onDelete();
            }}
          >
            <DeleteIcon fontSize="small" />
          </IconButton>
        </Stack>
      </Stack>
    </Box>
  );
}

function StatusChip({ status }: { status: FlightStatus }) {
  const color = statusColor(status);
  return <Chip label={status} size="small" color={color} variant="outlined" />;
}

function statusColor(status: FlightStatus): 'default' | 'primary' | 'success' | 'warning' | 'error' {
  switch (status) {
    case 'Enroute':
    case 'Departed':
      return 'primary';
    case 'Arrived':
      return 'success';
    case 'Boarding':
      return 'warning';
    case 'Cancelled':
    case 'Diverted':
      return 'error';
    default:
      return 'default';
  }
}

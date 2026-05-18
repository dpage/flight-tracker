import {
  Avatar,
  AvatarGroup,
  Box,
  Chip,
  IconButton,
  Stack,
  Tooltip,
  Typography,
} from '@mui/material';
import EditIcon from '@mui/icons-material/Edit';
import DeleteIcon from '@mui/icons-material/DeleteOutline';

import { useStore } from '../state/store';
import type { Flight, FlightStatus, User } from '../api/types';

interface Props {
  onEditFlight: (id: number) => void;
}

export default function FlightList({ onEditFlight }: Props) {
  const flights = useStore((s) => s.flights);
  const users = useStore((s) => s.users);
  const selectedFlightId = useStore((s) => s.selectedFlightId);
  const selectFlight = useStore((s) => s.selectFlight);
  const deleteFlight = useStore((s) => s.deleteFlight);

  if (flights.length === 0) {
    return (
      <Box sx={{ p: 3 }}>
        <Typography color="text.secondary" variant="body2">
          No flights yet. Click <strong>Add flight</strong> in the top bar to track your first
          journey.
        </Typography>
      </Box>
    );
  }

  const usersById = new Map(users.map((u) => [u.id, u]));

  return (
    <Stack divider={<Box sx={{ borderBottom: 1, borderColor: 'divider' }} />}>
      {flights.map((f) => (
        <FlightRow
          key={f.id}
          flight={f}
          passengers={f.passenger_ids
            .map((id) => usersById.get(id))
            .filter((u): u is User => u !== undefined)}
          selected={f.id === selectedFlightId}
          onSelect={() => selectFlight(f.id === selectedFlightId ? null : f.id)}
          onEdit={() => onEditFlight(f.id)}
          onDelete={() => {
            if (confirm(`Delete flight ${f.ident}?`)) void deleteFlight(f.id);
          }}
        />
      ))}
    </Stack>
  );
}

interface FlightRowProps {
  flight: Flight;
  passengers: User[];
  selected: boolean;
  onSelect: () => void;
  onEdit: () => void;
  onDelete: () => void;
}

function FlightRow({ flight, passengers, selected, onSelect, onEdit, onDelete }: FlightRowProps) {
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
            {fmtDateTime(flight.scheduled_out)} → {fmtDateTime(eta)}
          </Typography>
          {passengers.length > 0 && (
            <AvatarGroup
              max={6}
              sx={{ mt: 0.5, justifyContent: 'flex-start', '& .MuiAvatar-root': { width: 24, height: 24, fontSize: 12 } }}
            >
              {passengers.map((u) => (
                <Tooltip key={u.id} title={u.github_login}>
                  <Avatar src={u.avatar_url}>{u.github_login.charAt(0).toUpperCase()}</Avatar>
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

function fmtDateTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

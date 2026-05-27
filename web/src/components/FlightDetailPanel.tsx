import { useEffect, useState } from 'react';
import { Avatar, Box, Chip, Stack, Typography } from '@mui/material';

import type { Flight, User } from '../api/types';
import { fmtAgo, fmtDateTime, fmtUTC } from '../lib/format';

interface Props {
  flight: Flight;
  passengers: User[];
  sharedWith: User[];
  owner: User | undefined;
}

// FlightDetailPanel renders the expanded detail block shown beneath the
// currently-selected flight in the sidebar. Pure presentation: it does not
// touch the store. Field groups (Aircraft, Schedule, Current position, …)
// collapse to nothing when the underlying data is missing — the panel for a
// freshly-scheduled flight with no telemetry yet is correspondingly short.
export default function FlightDetailPanel({ flight, passengers, sharedWith, owner }: Props) {
  // Tick once a second so "Fix age" / "Last polled" stay live.
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, []);

  const pos = flight.latest_position;
  const fixAgeSec = pos ? Math.max(0, Math.floor((now - new Date(pos.ts).getTime()) / 1000)) : 0;
  const fixIsStale = pos && fixAgeSec > 5 * 60;

  return (
    <Box
      data-testid="flight-detail-panel"
      sx={{
        px: 2,
        py: 1.5,
        bgcolor: 'action.hover',
        borderLeft: 3,
        borderLeftColor: 'primary.main',
        borderBottom: 1,
        borderBottomColor: 'divider',
      }}
    >
      <Stack spacing={1.5}>
        <Section title="Aircraft">
          <Row label="ICAO24" value={flight.icao24 ? <Mono>{flight.icao24}</Mono> : null} />
          <Row label="Notes" value={flight.notes || null} />
        </Section>

        <Section title="Schedule">
          <TimeRow label="Scheduled out" iso={flight.scheduled_out} tz={flight.origin_tz} />
          <TimeRow label="Estimated out" iso={flight.estimated_out} tz={flight.origin_tz} />
          <TimeRow label="Actual out" iso={flight.actual_out} tz={flight.origin_tz} />
          <TimeRow label="Scheduled in" iso={flight.scheduled_in} tz={flight.dest_tz} />
          <TimeRow label="Estimated in" iso={flight.estimated_in} tz={flight.dest_tz} />
          <TimeRow label="Actual in" iso={flight.actual_in} tz={flight.dest_tz} />
        </Section>

        {pos && (
          <Section
            title="Current position"
            titleAdornment={
              pos.is_estimated ? (
                <Chip
                  label="estimated"
                  size="small"
                  color="warning"
                  variant="outlined"
                  sx={{ height: 18, fontSize: 10 }}
                />
              ) : null
            }
          >
            <Row label="Latitude" value={`${pos.lat.toFixed(4)}°`} />
            <Row label="Longitude" value={`${pos.lon.toFixed(4)}°`} />
            <Row
              label="Altitude"
              value={pos.altitude_ft != null ? `${pos.altitude_ft.toLocaleString()} ft` : null}
            />
            <Row
              label="Groundspeed"
              value={pos.groundspeed_kt != null ? `${pos.groundspeed_kt} kt` : null}
            />
            <Row label="Heading" value={pos.heading_deg != null ? `${pos.heading_deg}°` : null} />
            <Row
              label="Fix age"
              value={
                <Typography
                  variant="body2"
                  component="span"
                  color={fixIsStale ? 'warning.main' : 'text.primary'}
                >
                  {fmtAgo(pos.ts, now)}
                </Typography>
              }
            />
          </Section>
        )}

        {(passengers.length > 0 || sharedWith.length > 0 || owner) && (
          <Section title="People">
            {passengers.length > 0 && (
              <Row
                label="Passengers"
                value={
                  <Stack spacing={0.5}>
                    {passengers.map((p) => (
                      <UserChip key={p.id} user={p} />
                    ))}
                  </Stack>
                }
              />
            )}
            {sharedWith.length > 0 && (
              <Row
                label="Shared with"
                value={
                  <Stack spacing={0.5}>
                    {sharedWith.map((u) => (
                      <UserChip key={u.id} user={u} />
                    ))}
                  </Stack>
                }
              />
            )}
            {owner && <Row label="Added by" value={<UserChip user={owner} />} />}
          </Section>
        )}

        <Section title="Visibility">
          <Row
            label="Audience"
            value={
              flight.is_public ? (
                <Chip
                  label="Public — everyone"
                  size="small"
                  color="primary"
                  variant="outlined"
                  sx={{ height: 20, fontSize: 11 }}
                />
              ) : (
                <Typography variant="body2" color="text.secondary">
                  Creator, passengers{sharedWith.length > 0 ? ', and shared users' : ''}
                </Typography>
              )
            }
          />
        </Section>

        <Section title="Polling">
          <Row
            label="Last polled"
            value={flight.last_polled_at ? fmtAgo(flight.last_polled_at, now) : 'never'}
          />
        </Section>
      </Stack>
    </Box>
  );
}

function Section({
  title,
  titleAdornment,
  children,
}: {
  title: string;
  titleAdornment?: React.ReactNode;
  children: React.ReactNode;
}) {
  // Drop the section entirely if every Row inside chose to render nothing —
  // keeps the panel tight for flights with sparse data.
  const visibleChildren = (Array.isArray(children) ? children : [children]).filter(
    (c): c is React.ReactElement => c != null && c !== false,
  );
  if (visibleChildren.length === 0) return null;
  return (
    <Box>
      <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 0.5 }}>
        <Typography
          variant="overline"
          color="text.secondary"
          sx={{ lineHeight: 1.2, letterSpacing: 0.8 }}
        >
          {title}
        </Typography>
        {titleAdornment}
      </Stack>
      <Stack spacing={0.5}>{children}</Stack>
    </Box>
  );
}

// Row hides itself when value is null/undefined/'' — callers can pass raw
// values and the panel auto-collapses missing fields.
function Row({ label, value }: { label: string; value: React.ReactNode | null | undefined }) {
  if (value == null || value === '') return null;
  return (
    <Stack direction="row" spacing={1} alignItems="flex-start">
      <Typography
        variant="caption"
        color="text.secondary"
        sx={{ width: 96, flexShrink: 0, pt: 0.25 }}
      >
        {label}
      </Typography>
      <Box sx={{ flexGrow: 1, minWidth: 0 }}>
        {typeof value === 'string' || typeof value === 'number' ? (
          <Typography variant="body2">{value}</Typography>
        ) : (
          value
        )}
      </Box>
    </Stack>
  );
}

// TimeRow renders airport-local time on the first line and a faint UTC line
// beneath. Hidden when iso is missing.
function TimeRow({ label, iso, tz }: { label: string; iso?: string; tz?: string }) {
  if (!iso) return null;
  return (
    <Row
      label={label}
      value={
        <Box>
          <Typography variant="body2">{fmtDateTime(iso, tz)}</Typography>
          {tz && (
            <Typography variant="caption" color="text.secondary">
              {fmtUTC(iso)}
            </Typography>
          )}
        </Box>
      }
    />
  );
}

function UserChip({ user }: { user: User }) {
  return (
    <Stack direction="row" spacing={0.75} alignItems="center">
      <Avatar src={user.avatar_url} sx={{ width: 20, height: 20, fontSize: 11 }}>
        {user.username.charAt(0).toUpperCase()}
      </Avatar>
      <Typography variant="body2">{user.username}</Typography>
    </Stack>
  );
}

function Mono({ children }: { children: React.ReactNode }) {
  return (
    <Typography variant="body2" component="span" sx={{ fontFamily: 'monospace' }}>
      {children}
    </Typography>
  );
}

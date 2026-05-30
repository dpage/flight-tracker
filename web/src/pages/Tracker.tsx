import { useEffect, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  Box,
  Chip,
  Divider,
  FormControl,
  InputLabel,
  List,
  ListItemButton,
  ListItemText,
  MenuItem,
  Select,
  Slider,
  Stack,
  Typography,
} from '@mui/material';

import { useStore } from '../state/store';
import { tripSpan } from '../lib/trip-format';
import TrackerMap from '../components/TrackerMap';

/** Parse a window token like "7d" into a day count. Defaults to 7 on garbage. */
function parseDays(token: string): number {
  const m = /^(\d+)d$/.exec(token.trim());
  if (!m) return 7;
  return Math.max(0, Math.min(60, Number(m[1])));
}

function days(n: number): string {
  return `${n}d`;
}

const DAY_MS = 24 * 60 * 60 * 1000;

/** Tracker convergence view (PRD §6.5). Two modes, selected by how it's opened:
 *
 * - `?part={id}` → single-flight focus: the map fits to that one part and the
 *   list narrows to it.
 * - otherwise → the "who's on their way" map of every in-window trackable part
 *   across visible trips, with a list alongside. No ranking/leaderboard.
 *
 * A tag selector scopes the view to a shared tag and seeds the default window
 * from that tag's trip span (§6.6); the −/+ day sliders then widen or narrow it,
 * persisted per-tag to localStorage via the store. */
export default function Tracker() {
  const [searchParams] = useSearchParams();
  const focusedPartId = useMemo(() => {
    const raw = searchParams.get('part');
    if (raw == null) return null;
    const n = Number(raw);
    return Number.isFinite(n) ? n : null;
  }, [searchParams]);

  const loadTracker = useStore((s) => s.loadTracker);
  const setTrackerWindow = useStore((s) => s.setTrackerWindow);
  const parts = useStore((s) => s.trackerParts);
  const tag = useStore((s) => s.trackerTag);
  const win = useStore((s) => s.trackerWindow);
  const loading = useStore((s) => s.trackerLoading);
  const trips = useStore((s) => s.trips);
  const listTrips = useStore((s) => s.listTrips);

  useEffect(() => {
    void loadTracker();
  }, [loadTracker]);

  // The tag selector's options come from the tags on trips the viewer can see;
  // we need the trip list for both that and the tag-derived default window.
  useEffect(() => {
    if (trips.length === 0) void listTrips();
  }, [trips.length, listTrips]);

  const tagOptions = useMemo(() => {
    const set = new Set<string>();
    for (const t of trips) for (const label of t.tags) set.add(label);
    return [...set].sort();
  }, [trips]);

  // Default window spanning the tagged trips the viewer can see (§6.6): the
  // furthest-back start and furthest-forward end, expressed as whole days
  // before/after now. The user can still widen/narrow with the sliders.
  const tagWindow = (label: string): { before: string; after: string } | null => {
    if (!label) return null;
    const now = Date.now();
    let before = 0;
    let after = 0;
    let any = false;
    for (const t of trips) {
      if (!t.tags.includes(label)) continue;
      const span = tripSpan(t);
      if (span.start != null) {
        before = Math.max(before, Math.ceil((now - span.start) / DAY_MS));
        any = true;
      }
      if (span.end != null) {
        after = Math.max(after, Math.ceil((span.end - now) / DAY_MS));
        any = true;
      }
    }
    if (!any) return null;
    // Pad by a day so trips that start/end today aren't clipped, and clamp.
    return {
      before: days(Math.max(1, Math.min(60, before + 1))),
      after: days(Math.max(1, Math.min(60, after + 1))),
    };
  };

  const onTagChange = (label: string) => {
    const seeded = tagWindow(label);
    if (seeded) {
      // Seed the window from the tag's span, then reload for the tag.
      void setTrackerWindow(seeded).then(() => loadTracker({ tag: label }));
    } else {
      void loadTracker({ tag: label });
    }
  };

  const before = parseDays(win.before);
  const after = parseDays(win.after);

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <Box sx={{ px: 3, pt: 2, pb: 1 }}>
        <Stack direction="row" alignItems="center" spacing={2} sx={{ mb: 1 }}>
          <Typography variant="h5" sx={{ flexGrow: 1 }}>
            Tracker
          </Typography>
          {focusedPartId != null && (
            <Chip label="Single flight" color="primary" size="small" variant="outlined" />
          )}
        </Stack>

        {focusedPartId == null && (
          <Stack
            direction={{ xs: 'column', sm: 'row' }}
            spacing={3}
            alignItems={{ sm: 'center' }}
            sx={{ mb: 1 }}
          >
            <FormControl size="small" sx={{ minWidth: 200 }}>
              <InputLabel id="tracker-tag-label">Tag</InputLabel>
              <Select
                labelId="tracker-tag-label"
                label="Tag"
                value={tag}
                onChange={(e) => onTagChange(e.target.value)}
              >
                <MenuItem value="">
                  <em>Everyone (untagged view)</em>
                </MenuItem>
                {tagOptions.map((label) => (
                  <MenuItem key={label} value={label}>
                    {label}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>

            <Box sx={{ minWidth: 180 }}>
              <Typography variant="caption" color="text.secondary" id="tracker-before-label">
                From {before}d before
              </Typography>
              <Slider
                aria-labelledby="tracker-before-label"
                value={before}
                min={0}
                max={30}
                step={1}
                marks={[
                  { value: 0, label: 'now' },
                  { value: 7, label: '7d' },
                  { value: 30, label: '30d' },
                ]}
                onChangeCommitted={(_e, v) => void setTrackerWindow({ before: days(v as number) })}
              />
            </Box>

            <Box sx={{ minWidth: 180 }}>
              <Typography variant="caption" color="text.secondary" id="tracker-after-label">
                To {after}d after
              </Typography>
              <Slider
                aria-labelledby="tracker-after-label"
                value={after}
                min={0}
                max={30}
                step={1}
                marks={[
                  { value: 0, label: 'now' },
                  { value: 7, label: '7d' },
                  { value: 30, label: '30d' },
                ]}
                onChangeCommitted={(_e, v) => void setTrackerWindow({ after: days(v as number) })}
              />
            </Box>
          </Stack>
        )}
      </Box>

      <Divider />

      <Box sx={{ flexGrow: 1, minHeight: 0, display: 'flex', flexDirection: { xs: 'column', md: 'row' } }}>
        <Box sx={{ position: 'relative', flexGrow: 1, minHeight: 280 }}>
          <TrackerMap parts={parts} focusedPartId={focusedPartId} />
        </Box>
        <Box
          sx={{
            width: { xs: '100%', md: 300 },
            borderLeft: { md: 1 },
            borderTop: { xs: 1, md: 0 },
            borderColor: 'divider',
            overflowY: 'auto',
          }}
        >
          <TrackerList parts={parts} focusedPartId={focusedPartId} loading={loading} />
        </Box>
      </Box>
    </Box>
  );
}

interface TrackerListProps {
  parts: import('../api/types').TrackerPart[];
  focusedPartId: number | null;
  loading: boolean;
}

function TrackerList({ parts, focusedPartId, loading }: TrackerListProps) {
  const [, setSearchParams] = useSearchParams();
  const shown = focusedPartId != null ? parts.filter((p) => p.plan_part_id === focusedPartId) : parts;

  if (shown.length === 0) {
    return (
      <Box sx={{ p: 2 }}>
        <Typography variant="body2" color="text.secondary">
          {loading ? 'Loading…' : 'No travel in this window.'}
        </Typography>
      </Box>
    );
  }

  return (
    <List dense disablePadding>
      {shown.map((p) => {
        const pos = p.latest_position;
        const secondary = [
          p.dest_iata ? `→ ${p.dest_iata}` : '',
          p.status,
          pos?.is_estimated ? '(estimated)' : '',
        ]
          .filter(Boolean)
          .join(' · ');
        return (
          <ListItemButton
            key={p.plan_part_id}
            selected={focusedPartId === p.plan_part_id}
            onClick={() =>
              setSearchParams(
                focusedPartId === p.plan_part_id ? {} : { part: String(p.plan_part_id) },
              )
            }
          >
            <ListItemText
              primary={p.ident || p.title || `#${p.plan_part_id}`}
              secondary={secondary || undefined}
            />
            {pos && (
              <Chip
                size="small"
                label="live"
                color={pos.is_estimated ? 'default' : 'success'}
                variant="outlined"
              />
            )}
          </ListItemButton>
        );
      })}
    </List>
  );
}

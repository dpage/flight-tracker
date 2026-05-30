import { useEffect, useState } from 'react';
import { Outlet, useLocation, useNavigate, useParams } from 'react-router-dom';
import { Box, Button, Stack, Tab, Tabs, Typography } from '@mui/material';
import CalendarMonthIcon from '@mui/icons-material/CalendarMonth';

import { useStore } from '../state/store';
import TagInput from '../components/TagInput';
import CalendarSubscribeDialog from '../components/CalendarSubscribeDialog';

/** Trip detail layout (spec §11). Holds the Timeline / Map sub-tabs and loads
 * the trip into the store on mount; the active tab renders via the nested
 * route `<Outlet>`. Wave 0b wires loading + tab navigation; the tab bodies are
 * placeholders fleshed out in Wave 1F. */
export default function TripDetail() {
  const params = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const tripId = Number(params.id);

  const currentTrip = useStore((s) => s.currentTrip);
  const loadTrip = useStore((s) => s.loadTrip);
  const clearCurrentTrip = useStore((s) => s.clearCurrentTrip);
  const setTripTags = useStore((s) => s.setTripTags);

  const [subscribeOpen, setSubscribeOpen] = useState(false);

  useEffect(() => {
    if (!Number.isFinite(tripId)) return;
    void loadTrip(tripId);
    return () => clearCurrentTrip();
  }, [tripId, loadTrip, clearCurrentTrip]);

  const onMap = location.pathname.endsWith('/map');
  const tab = onMap ? 'map' : 'timeline';
  const loaded = currentTrip?.id === tripId ? currentTrip : null;
  const title = loaded ? loaded.name : `Trip #${tripId}`;
  // Only owners/editors get the tag editor; viewers see nothing to change.
  const canEdit = loaded != null && loaded.my_role !== 'viewer';

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <Box sx={{ px: 3, pt: 2, display: 'flex', alignItems: 'center', gap: 2 }}>
        <Button size="small" onClick={() => navigate('/')}>
          ← Trips
        </Button>
        <Typography variant="h5" sx={{ flexGrow: 1 }}>
          {title}
        </Typography>
        <Button
          size="small"
          startIcon={<CalendarMonthIcon />}
          onClick={() => setSubscribeOpen(true)}
        >
          Subscribe
        </Button>
      </Box>
      {loaded && canEdit && (
        <Box sx={{ px: 3, pt: 1.5 }}>
          <Stack sx={{ maxWidth: 520 }}>
            <TagInput
              value={loaded.tags}
              onChange={(labels) => void setTripTags(tripId, labels)}
              helperText="Tags group trips so people find each other — they never grant access (PRD §6.6)."
            />
          </Stack>
        </Box>
      )}
      <Tabs
        value={tab}
        onChange={(_e, v) => navigate(v === 'map' ? `/trips/${tripId}/map` : `/trips/${tripId}`)}
        sx={{ px: 3, borderBottom: 1, borderColor: 'divider' }}
      >
        <Tab label="Timeline" value="timeline" />
        <Tab label="Map" value="map" />
      </Tabs>
      <Box sx={{ flexGrow: 1, minHeight: 0, overflowY: 'auto' }}>
        <Outlet />
      </Box>

      <CalendarSubscribeDialog
        open={subscribeOpen}
        onClose={() => setSubscribeOpen(false)}
        scope="trip"
        id={tripId}
        title={title}
      />
    </Box>
  );
}

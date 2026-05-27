import { useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Autocomplete,
  Avatar,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControlLabel,
  LinearProgress,
  Link,
  Stack,
  Switch,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import { DatePicker } from '@mui/x-date-pickers/DatePicker';
import { DateTimePicker } from '@mui/x-date-pickers/DateTimePicker';

import { api } from '../api/client';
import { useStore } from '../state/store';
import type { CreateFlightInput, FlightStatus, User } from '../api/types';

interface Props {
  open: boolean;
  editId: number | null;
  onClose: () => void;
}

interface FormState {
  ident: string;
  icao24: string;
  scheduledOut: Date | null;
  scheduledIn: Date | null;
  originIATA: string;
  destIATA: string;
  status: FlightStatus;
  notes: string;
  passengers: User[];
  sharedWith: User[];
  isPublic: boolean;
}

interface MinimalState {
  ident: string;
  date: Date | null;
  notes: string;
  passengers: User[];
  sharedWith: User[];
  isPublic: boolean;
}

const STATUSES: FlightStatus[] = [
  'Scheduled',
  'Boarding',
  'Departed',
  'Enroute',
  'Arrived',
  'Cancelled',
  'Diverted',
];

export default function FlightDialog({ open, editId, onClose }: Props) {
  const users = useStore((s) => s.users);
  const flights = useStore((s) => s.flights);
  const me = useStore((s) => s.me);
  const capabilities = useStore((s) => s.capabilities);
  const createFlight = useStore((s) => s.createFlight);
  const updateFlight = useStore((s) => s.updateFlight);
  const addPassenger = useStore((s) => s.addPassenger);
  const removePassenger = useStore((s) => s.removePassenger);
  const addShare = useStore((s) => s.addShare);
  const removeShare = useStore((s) => s.removeShare);
  const setError = useStore((s) => s.setError);

  const editing = useMemo(
    () => (editId == null ? null : flights.find((f) => f.id === editId) ?? null),
    [editId, flights],
  );

  // The minimal "ident + date" form is used for new flights when the server
  // has a Resolver wired AND the user hasn't asked to enter everything by
  // hand. Editing always uses the full form.
  const [manualOverride, setManualOverride] = useState(false);
  const useMinimal = !editing && capabilities.resolver_available && !manualOverride;

  const [form, setForm] = useState<FormState>(emptyForm());
  const [minimal, setMinimal] = useState<MinimalState>(emptyMinimal());
  const [busy, setBusy] = useState(false);
  // submitStatus drives the LinearProgress + caption strip across the top
  // of the dialog while a submit is in flight. "" means idle/no progress
  // bar. The resolver path can take several seconds (rate-limited API
  // call + auto-retry on 429) so a visible "doing something" affordance
  // matters here.
  const [submitStatus, setSubmitStatus] = useState('');
  // Resolver failures used to auto-drop into the manual form, which made it
  // very easy to accidentally Create with blank IATAs. We now show the
  // failure inline and require an explicit choice (Try again / Enter
  // manually / Cancel) so nothing happens implicitly.
  const [resolveError, setResolveError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setManualOverride(false);
    setResolveError(null);
    setSubmitStatus('');
    if (editing) {
      const passengers = editing.passenger_ids
        .map((id) => users.find((u) => u.id === id))
        .filter((u): u is User => u !== undefined);
      const sharedWith = (editing.shared_user_ids ?? [])
        .map((id) => users.find((u) => u.id === id))
        .filter((u): u is User => u !== undefined);
      setForm({
        ident: editing.ident,
        icao24: editing.icao24 ?? '',
        scheduledOut: new Date(editing.scheduled_out),
        scheduledIn: new Date(editing.scheduled_in),
        originIATA: editing.origin_iata,
        destIATA: editing.dest_iata,
        status: editing.status,
        notes: editing.notes,
        passengers,
        sharedWith,
        isPublic: editing.is_public ?? false,
      });
    } else {
      setForm(emptyForm());
      setMinimal(emptyMinimal());
    }
  }, [open, editing, users]);

  // Only the creator and superusers can change visibility/sharing on an
  // existing flight. New flights have no creator yet, so anyone can set
  // the initial values.
  const canEditSharing =
    editing == null ||
    (me != null && (me.is_superuser || me.id === editing.created_by));

  const canSubmitFull =
    form.ident.trim() !== '' &&
    form.scheduledOut !== null &&
    form.scheduledIn !== null &&
    form.scheduledIn > form.scheduledOut;
  const canSubmitMinimal = minimal.ident.trim() !== '' && minimal.date !== null;

  const handleFullSubmit = async () => {
    if (!canSubmitFull || !form.scheduledOut || !form.scheduledIn) return;
    setBusy(true);
    setSubmitStatus(editing ? 'Saving changes…' : 'Creating flight…');
    try {
      if (editing) {
        const patch: Parameters<typeof updateFlight>[1] = {};
        const originIATA = form.originIATA.trim().toUpperCase();
        const destIATA = form.destIATA.trim().toUpperCase();
        if (form.scheduledOut.getTime() !== new Date(editing.scheduled_out).getTime()) {
          patch.scheduled_out = form.scheduledOut.toISOString();
        }
        if (form.scheduledIn.getTime() !== new Date(editing.scheduled_in).getTime()) {
          patch.scheduled_in = form.scheduledIn.toISOString();
        }
        if (originIATA !== editing.origin_iata) patch.origin_iata = originIATA;
        if (destIATA !== editing.dest_iata) patch.dest_iata = destIATA;
        if (form.icao24.trim().toLowerCase() !== (editing.icao24 ?? '').toLowerCase()) {
          patch.icao24 = form.icao24.trim().toLowerCase();
        }
        if (form.notes !== editing.notes) patch.notes = form.notes;
        if (form.status !== editing.status) patch.status = form.status;
        if (canEditSharing && form.isPublic !== editing.is_public) {
          patch.is_public = form.isPublic;
        }
        if (Object.keys(patch).length > 0) await updateFlight(editing.id, patch);
        const existing = new Set(editing.passenger_ids);
        const next = new Set(form.passengers.map((u) => u.id));
        for (const uid of next) if (!existing.has(uid)) await addPassenger(editing.id, uid);
        for (const uid of existing) if (!next.has(uid)) await removePassenger(editing.id, uid);
        if (canEditSharing) {
          const existingShared = new Set(editing.shared_user_ids ?? []);
          const nextShared = new Set(form.sharedWith.map((u) => u.id));
          for (const uid of nextShared) {
            if (!existingShared.has(uid)) await addShare(editing.id, uid);
          }
          for (const uid of existingShared) {
            if (!nextShared.has(uid)) await removeShare(editing.id, uid);
          }
        }
      } else {
        const input: CreateFlightInput = {
          ident: form.ident.trim().toUpperCase(),
          icao24: form.icao24.trim().toLowerCase() || undefined,
          scheduled_out: form.scheduledOut.toISOString(),
          scheduled_in: form.scheduledIn.toISOString(),
          origin_iata: form.originIATA.trim().toUpperCase(),
          dest_iata: form.destIATA.trim().toUpperCase(),
          notes: form.notes,
          passenger_ids: form.passengers.map((u) => u.id),
          shared_user_ids: form.sharedWith.map((u) => u.id),
          is_public: form.isPublic,
        };
        await createFlight(input);
      }
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
      setSubmitStatus('');
    }
  };

  const handleMinimalSubmit = async () => {
    if (!canSubmitMinimal || !minimal.date) return;
    setBusy(true);
    setResolveError(null);
    setSubmitStatus(`Looking up ${minimal.ident.trim().toUpperCase()}…`);
    try {
      const resolved = await api.resolveFlight({
        ident: minimal.ident.trim().toUpperCase(),
        date: formatDateOnly(minimal.date),
      });
      setSubmitStatus(`Found ${resolved.ident} — creating flight…`);
      const input: CreateFlightInput = {
        ident: resolved.ident,
        scheduled_out: resolved.scheduled_out,
        scheduled_in: resolved.scheduled_in,
        origin_iata: resolved.origin_iata,
        dest_iata: resolved.dest_iata,
        icao24: resolved.icao24 || undefined,
        notes: minimal.notes || resolved.notes,
        passenger_ids: minimal.passengers.map((u) => u.id),
        shared_user_ids: minimal.sharedWith.map((u) => u.id),
        is_public: minimal.isPublic,
      };
      await createFlight(input);
      onClose();
    } catch (err) {
      // Stay on the minimal form and surface the error inline. The user
      // explicitly picks the next step via the buttons in DialogActions —
      // we deliberately do NOT auto-switch to the manual form because that
      // path used to swallow IATAs on accidental submits.
      setResolveError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
      setSubmitStatus('');
    }
  };

  const switchToManual = () => {
    setForm((f) => ({
      ...f,
      ident: minimal.ident.trim().toUpperCase(),
      notes: minimal.notes,
      passengers: minimal.passengers,
      sharedWith: minimal.sharedWith,
      isPublic: minimal.isPublic,
      scheduledOut: minimal.date,
      scheduledIn: minimal.date ? new Date(minimal.date.getTime() + 2 * 60 * 60 * 1000) : null,
    }));
    setResolveError(null);
    setManualOverride(true);
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>{editing ? `Edit ${editing.ident}` : 'Add flight'}</DialogTitle>
      {busy && (
        <Box>
          <LinearProgress />
          {submitStatus && (
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ display: 'block', px: 3, py: 0.75 }}
            >
              {submitStatus}
            </Typography>
          )}
        </Box>
      )}
      <DialogContent dividers>
        {!editing && capabilities.email_ingest_address && (
          <Alert severity="info" variant="outlined" sx={{ mb: 2 }}>
            You can also email your itinerary to{' '}
            <Link href={`mailto:${capabilities.email_ingest_address}`}>
              {capabilities.email_ingest_address}
            </Link>{' '}
            and the flight will be added automatically.
          </Alert>
        )}
        {useMinimal ? (
          <Stack spacing={2} sx={{ pt: 1 }}>
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
              <TextField
                label="Flight number"
                value={minimal.ident}
                onChange={(e) => setMinimal({ ...minimal, ident: e.target.value })}
                autoFocus
                required
                placeholder="e.g. BA286"
                inputProps={{ style: { textTransform: 'uppercase' } }}
                sx={{ flex: 1 }}
              />
              <DatePicker
                label="Departure date (at origin)"
                value={minimal.date}
                onChange={(d) => setMinimal({ ...minimal, date: d })}
                sx={{ flex: 1 }}
              />
            </Stack>
            {resolveError ? (
              <Alert severity="warning" variant="outlined">
                Couldn’t look up that flight: {resolveError}. Try a different
                ident or date, or click <strong>Enter manually</strong> to
                fill in the rest of the details by hand.
              </Alert>
            ) : (
              <Box sx={{ color: 'text.secondary', fontSize: 13 }}>
                Schedule, airports, and aircraft details will be filled in
                from the flight database. Switch to{' '}
                <Link
                  component="button"
                  type="button"
                  onClick={() => setManualOverride(true)}
                  sx={{ verticalAlign: 'baseline' }}
                >
                  manual entry
                </Link>{' '}
                if you want to enter a flight that isn’t in the database.
              </Box>
            )}
            <Autocomplete
              multiple
              options={users}
              value={minimal.passengers}
              getOptionLabel={(o) => o.username}
              isOptionEqualToValue={(a, b) => a.id === b.id}
              onChange={(_, value) => setMinimal({ ...minimal, passengers: value })}
              renderTags={(value, getTagProps) =>
                value.map((u, i) => (
                  <Chip
                    {...getTagProps({ index: i })}
                    key={u.id}
                    avatar={
                      <Avatar src={u.avatar_url}>
                        {u.username.charAt(0).toUpperCase()}
                      </Avatar>
                    }
                    label={u.username}
                  />
                ))
              }
              renderInput={(params) => <TextField {...params} label="Passengers" />}
            />
            <TextField
              label="Notes (optional)"
              value={minimal.notes}
              onChange={(e) => setMinimal({ ...minimal, notes: e.target.value })}
              multiline
              rows={2}
              helperText="Leaving notes blank uses the resolver’s default (airline + aircraft model)"
            />
            <VisibilityBlock
              users={users}
              sharedWith={minimal.sharedWith}
              isPublic={minimal.isPublic}
              disabled={false}
              onSharedChange={(value) => setMinimal({ ...minimal, sharedWith: value })}
              onPublicChange={(value) => setMinimal({ ...minimal, isPublic: value })}
            />
          </Stack>
        ) : (
          <Stack spacing={2} sx={{ pt: 1 }}>
            {!editing && capabilities.resolver_available && (
              <Typography variant="body2" color="text.secondary">
                Entering everything manually.{' '}
                <Link
                  component="button"
                  type="button"
                  onClick={() => setManualOverride(false)}
                  sx={{ verticalAlign: 'baseline' }}
                >
                  Look it up instead
                </Link>
              </Typography>
            )}
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
              <TextField
                label="Flight number"
                value={form.ident}
                onChange={(e) => setForm({ ...form, ident: e.target.value })}
                disabled={editing !== null}
                autoFocus
                required
                placeholder="e.g. BA286"
                sx={{ flex: 2 }}
              />
              <TextField
                label="ICAO24 (optional)"
                value={form.icao24}
                onChange={(e) => setForm({ ...form, icao24: e.target.value })}
                placeholder="e.g. 400a1d"
                inputProps={{
                  maxLength: 6,
                  style: { textTransform: 'lowercase', fontFamily: 'monospace' },
                }}
                helperText="6-char hex aircraft ID for live position lookup"
                sx={{ flex: 1 }}
              />
            </Stack>
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
              <DateTimePicker
                label="Scheduled departure (your local time)"
                value={form.scheduledOut}
                onChange={(d) => setForm({ ...form, scheduledOut: d })}
                ampm={false}
                sx={{ flexGrow: 1 }}
              />
              <DateTimePicker
                label="Scheduled arrival (your local time)"
                value={form.scheduledIn}
                onChange={(d) => setForm({ ...form, scheduledIn: d })}
                ampm={false}
                sx={{ flexGrow: 1 }}
              />
            </Stack>
            <Stack direction="row" spacing={2}>
              <TextField
                label="Origin IATA"
                value={form.originIATA}
                onChange={(e) => setForm({ ...form, originIATA: e.target.value })}
                placeholder="LHR"
                inputProps={{ maxLength: 4, style: { textTransform: 'uppercase' } }}
                sx={{ flexGrow: 1 }}
              />
              <TextField
                label="Destination IATA"
                value={form.destIATA}
                onChange={(e) => setForm({ ...form, destIATA: e.target.value })}
                placeholder="JFK"
                inputProps={{ maxLength: 4, style: { textTransform: 'uppercase' } }}
                sx={{ flexGrow: 1 }}
              />
            </Stack>
            {editing && (
              <TextField
                label="Status"
                select
                SelectProps={{ native: true }}
                value={form.status}
                onChange={(e) => setForm({ ...form, status: e.target.value })}
              >
                {STATUSES.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </TextField>
            )}
            <Autocomplete
              multiple
              options={users}
              value={form.passengers}
              getOptionLabel={(o) => o.username}
              isOptionEqualToValue={(a, b) => a.id === b.id}
              onChange={(_, value) => setForm({ ...form, passengers: value })}
              renderTags={(value, getTagProps) =>
                value.map((u, i) => (
                  <Chip
                    {...getTagProps({ index: i })}
                    key={u.id}
                    avatar={
                      <Avatar src={u.avatar_url}>
                        {u.username.charAt(0).toUpperCase()}
                      </Avatar>
                    }
                    label={u.username}
                  />
                ))
              }
              renderInput={(params) => <TextField {...params} label="Passengers" />}
            />
            <TextField
              label="Notes"
              value={form.notes}
              onChange={(e) => setForm({ ...form, notes: e.target.value })}
              multiline
              rows={2}
            />
            <VisibilityBlock
              users={users}
              sharedWith={form.sharedWith}
              isPublic={form.isPublic}
              disabled={!canEditSharing}
              onSharedChange={(value) => setForm({ ...form, sharedWith: value })}
              onPublicChange={(value) => setForm({ ...form, isPublic: value })}
            />
          </Stack>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        {useMinimal && resolveError && (
          <Button onClick={switchToManual} disabled={busy}>
            Enter manually
          </Button>
        )}
        <Button
          variant="contained"
          onClick={() => void (useMinimal ? handleMinimalSubmit() : handleFullSubmit())}
          disabled={busy || (useMinimal ? !canSubmitMinimal : !canSubmitFull)}
        >
          {editing
            ? 'Save'
            : useMinimal
              ? resolveError
                ? 'Try again'
                : 'Look up & add'
              : 'Create'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

function emptyForm(): FormState {
  const now = new Date();
  const dep = new Date(now);
  dep.setHours(dep.getHours() + 1, 0, 0, 0);
  const arr = new Date(dep);
  arr.setHours(arr.getHours() + 2);
  return {
    ident: '',
    icao24: '',
    scheduledOut: dep,
    scheduledIn: arr,
    originIATA: '',
    destIATA: '',
    status: 'Scheduled',
    notes: '',
    passengers: [],
    sharedWith: [],
    isPublic: false,
  };
}

function emptyMinimal(): MinimalState {
  return {
    ident: '',
    date: new Date(),
    notes: '',
    passengers: [],
    sharedWith: [],
    isPublic: false,
  };
}

interface VisibilityBlockProps {
  users: User[];
  sharedWith: User[];
  isPublic: boolean;
  disabled: boolean;
  onSharedChange: (next: User[]) => void;
  onPublicChange: (next: boolean) => void;
}

// VisibilityBlock renders the "Share with everyone" toggle + per-user share
// list, used by both the minimal and full FlightDialog forms. When public,
// the share list is dimmed and gets a helper note — it's still editable so
// un-toggling later doesn't lose the curated list.
function VisibilityBlock({
  users,
  sharedWith,
  isPublic,
  disabled,
  onSharedChange,
  onPublicChange,
}: VisibilityBlockProps) {
  const switchControl = (
    <FormControlLabel
      control={
        <Switch
          checked={isPublic}
          onChange={(e) => onPublicChange(e.target.checked)}
          disabled={disabled}
        />
      }
      label="Share with everyone"
    />
  );
  return (
    <Stack spacing={1}>
      {disabled ? (
        <Tooltip title="Only the flight's creator (or a superuser) can change sharing">
          <span>{switchControl}</span>
        </Tooltip>
      ) : (
        switchControl
      )}
      <Autocomplete
        multiple
        options={users}
        value={sharedWith}
        getOptionLabel={(o) => o.username}
        isOptionEqualToValue={(a, b) => a.id === b.id}
        onChange={(_, value) => onSharedChange(value)}
        disabled={disabled}
        renderTags={(value, getTagProps) =>
          value.map((u, i) => (
            <Chip
              {...getTagProps({ index: i })}
              key={u.id}
              avatar={<Avatar src={u.avatar_url}>{u.username.charAt(0).toUpperCase()}</Avatar>}
              label={u.username}
            />
          ))
        }
        renderInput={(params) => (
          <TextField
            {...params}
            label="Shared with"
            helperText={
              isPublic
                ? 'Flight is public — this list is ignored until you turn off "Share with everyone".'
                : 'Users listed here can see the flight in addition to its passengers.'
            }
          />
        )}
        sx={isPublic ? { opacity: 0.7 } : undefined}
      />
    </Stack>
  );
}

// formatDateOnly renders the user's picked calendar date as YYYY-MM-DD using
// local components — i.e. the date the user actually sees in the picker.
// The resolver (AeroDataBox) interprets the date as the local departure date
// at the origin airport, so this is the right frame: "what you pick is what
// gets looked up." Using UTC components here would silently shift the date
// for users east of UTC (e.g. Tokyo picking May 19 → sending May 18).
function formatDateOnly(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}

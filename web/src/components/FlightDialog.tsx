import { useEffect, useMemo, useState } from 'react';
import {
  Autocomplete,
  Avatar,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  TextField,
} from '@mui/material';
import { DateTimePicker } from '@mui/x-date-pickers/DateTimePicker';

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
  const createFlight = useStore((s) => s.createFlight);
  const updateFlight = useStore((s) => s.updateFlight);
  const addPassenger = useStore((s) => s.addPassenger);
  const removePassenger = useStore((s) => s.removePassenger);
  const setError = useStore((s) => s.setError);

  const editing = useMemo(
    () => (editId == null ? null : flights.find((f) => f.id === editId) ?? null),
    [editId, flights],
  );

  const [form, setForm] = useState<FormState>(emptyForm());
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!open) return;
    if (editing) {
      const passengers = editing.passenger_ids
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
      });
    } else {
      setForm(emptyForm());
    }
  }, [open, editing, users]);

  const canSubmit =
    form.ident.trim() !== '' &&
    form.scheduledOut !== null &&
    form.scheduledIn !== null &&
    form.scheduledIn > form.scheduledOut;

  const handleSubmit = async () => {
    if (!canSubmit || !form.scheduledOut || !form.scheduledIn) return;
    setBusy(true);
    try {
      if (editing) {
        // Send only fields the user actually changed. Anything we omit makes
        // the server fall back to either the existing value (most fields) or
        // a time-derived value (status).
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
        if (Object.keys(patch).length > 0) await updateFlight(editing.id, patch);
        const existing = new Set(editing.passenger_ids);
        const next = new Set(form.passengers.map((u) => u.id));
        for (const uid of next) if (!existing.has(uid)) await addPassenger(editing.id, uid);
        for (const uid of existing) if (!next.has(uid)) await removePassenger(editing.id, uid);
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
        };
        await createFlight(input);
      }
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>{editing ? `Edit ${editing.ident}` : 'Add flight'}</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={2} sx={{ pt: 1 }}>
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
              label="Scheduled departure (UTC)"
              value={form.scheduledOut}
              onChange={(d) => setForm({ ...form, scheduledOut: d })}
              ampm={false}
              sx={{ flexGrow: 1 }}
            />
            <DateTimePicker
              label="Scheduled arrival (UTC)"
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
            getOptionLabel={(o) => o.github_login}
            isOptionEqualToValue={(a, b) => a.id === b.id}
            onChange={(_, value) => setForm({ ...form, passengers: value })}
            renderTags={(value, getTagProps) =>
              value.map((u, i) => (
                <Chip
                  {...getTagProps({ index: i })}
                  key={u.id}
                  avatar={<Avatar src={u.avatar_url}>{u.github_login.charAt(0).toUpperCase()}</Avatar>}
                  label={u.github_login}
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
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="contained" onClick={() => void handleSubmit()} disabled={!canSubmit || busy}>
          {editing ? 'Save' : 'Create'}
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
  };
}

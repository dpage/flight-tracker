import { useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  FormControl,
  InputLabel,
  LinearProgress,
  Link,
  MenuItem,
  Select,
  Stack,
  Tab,
  Tabs,
  TextField,
  Typography,
} from '@mui/material';
import { DateTimePicker } from '@mui/x-date-pickers/DateTimePicker';

import { useStore } from '../state/store';
import { planTypeLabel } from '../lib/trip-format';
import PlanTypeIcon from './PlanTypeIcon';
import type {
  ConfirmPlanInput,
  CreatePlanInput,
  PlanPartInput,
  PlanType,
  ProposedPlan,
} from '../api/types';

interface AddToTripDialogProps {
  open: boolean;
  /** The trip to add the plan to; may be null when opened from the trip list
   * before a trip is chosen, in which case the dialog shows a trip picker. */
  tripId: number | null;
  onClose: () => void;
}

type CaptureTab = 'manual' | 'paste' | 'upload' | 'email';

const PLAN_TYPES: PlanType[] = ['flight', 'train', 'hotel', 'ground', 'dining', 'excursion'];

/** Confidence below this gets flagged in the confirm step (spec §6 — "anything
 * it's unsure about is flagged rather than silently guessed"). */
const LOW_CONFIDENCE = 0.6;

/** Capture dialog (spec §6 / §6.3): tabs Manual / Paste / Upload / From email.
 * Manual builds a CreatePlanInput and calls `createPlan`; paste/upload call
 * `ingest` and render the returned proposals in an editable confirm step
 * (low-confidence flags + proposed supersessions) before `confirmIngest`;
 * "From email" surfaces the forwarding address the backend exposes. */
export default function AddToTripDialog({ open, tripId, onClose }: AddToTripDialogProps) {
  const trips = useStore((s) => s.trips);
  const currentTrip = useStore((s) => s.currentTrip);
  const capabilities = useStore((s) => s.capabilities);
  const createPlan = useStore((s) => s.createPlan);
  const ingest = useStore((s) => s.ingest);
  const confirmIngest = useStore((s) => s.confirmIngest);
  const clearIngest = useStore((s) => s.clearIngest);
  const ingestProposals = useStore((s) => s.ingestProposals);
  const ingestBusy = useStore((s) => s.ingestBusy);
  const setError = useStore((s) => s.setError);

  const [tab, setTab] = useState<CaptureTab>('manual');
  // The trip the new plan(s) land in. Seeded from the prop / current trip, but
  // a null prop (the global "Add to trip") lets the user pick.
  const [selectedTrip, setSelectedTrip] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  // Flips once an ingest call has succeeded, handing the dialog over to the
  // confirm step. Driven separately from `ingestProposals` so an empty result
  // ("nothing found") still shows the confirm step's retry affordance.
  const [submitted, setSubmitted] = useState(false);

  // Reset transient state every time the dialog opens.
  useEffect(() => {
    if (!open) return;
    setTab('manual');
    setSelectedTrip(tripId ?? currentTrip?.id ?? null);
    setBusy(false);
    setSubmitted(false);
    clearIngest();
  }, [open, tripId, currentTrip, clearIngest]);

  const effectiveTripId = tripId ?? selectedTrip;

  const handleClose = () => {
    clearIngest();
    onClose();
  };

  const handleIngest = async (input: { text?: string; source: 'paste' | 'upload' }) => {
    if (effectiveTripId == null) return;
    setBusy(true);
    try {
      await ingest(effectiveTripId, { text: input.text, source: input.source });
      // Success: the store now holds the proposals; hand over to the confirm
      // step (which reads them from the store).
      setSubmitted(true);
    } catch {
      // `ingest` already pushed the message to the global snackbar; stay on
      // the input step so the user can retry or edit.
    } finally {
      setBusy(false);
    }
  };

  const handleManualCreate = async (input: CreatePlanInput) => {
    if (effectiveTripId == null) return;
    setBusy(true);
    try {
      await createPlan(effectiveTripId, input);
      handleClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const handleConfirm = async (plans: ConfirmPlanInput[]) => {
    if (effectiveTripId == null) return;
    setBusy(true);
    try {
      await confirmIngest(effectiveTripId, plans);
      handleClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  // The confirm step takes over the whole dialog once an ingest has returned.
  const inConfirm = submitted;
  const working = busy || ingestBusy;

  return (
    <Dialog open={open} onClose={handleClose} fullWidth maxWidth="sm">
      <DialogTitle>{inConfirm ? 'Confirm extracted plans' : 'Add to trip'}</DialogTitle>
      {working && <LinearProgress />}
      <DialogContent dividers>
        {!inConfirm && tripId == null && (
          <TripPicker trips={trips} value={selectedTrip} onChange={setSelectedTrip} />
        )}

        {inConfirm ? (
          <ConfirmStep
            proposals={ingestProposals}
            onCancel={() => {
              setSubmitted(false);
              clearIngest();
            }}
            onConfirm={handleConfirm}
            busy={working}
          />
        ) : (
          <>
            <Tabs
              value={tab}
              onChange={(_, v: CaptureTab) => setTab(v)}
              variant="fullWidth"
              sx={{ mb: 2 }}
            >
              <Tab value="manual" label="Manual" />
              <Tab value="paste" label="Paste text" />
              <Tab value="upload" label="Upload" />
              <Tab value="email" label="From email" />
            </Tabs>

            {effectiveTripId == null && tab !== 'email' && (
              <Alert severity="info" variant="outlined" sx={{ mb: 2 }}>
                Pick a trip above to add this plan to.
              </Alert>
            )}

            {tab === 'manual' && (
              <ManualTab
                disabled={effectiveTripId == null || working}
                onCreate={handleManualCreate}
              />
            )}
            {tab === 'paste' && (
              <PasteTab
                disabled={effectiveTripId == null || working}
                onIngest={(text) => void handleIngest({ text, source: 'paste' })}
              />
            )}
            {tab === 'upload' && (
              <UploadTab
                disabled={effectiveTripId == null || working}
                onIngest={(text) => void handleIngest({ text, source: 'upload' })}
              />
            )}
            {tab === 'email' && (
              <EmailTab
                enabled={capabilities.email_ingest_enabled}
                address={capabilities.email_ingest_address}
              />
            )}
          </>
        )}
      </DialogContent>
      {!inConfirm && (
        <DialogActions>
          <Button onClick={handleClose}>Cancel</Button>
        </DialogActions>
      )}
    </Dialog>
  );
}

interface TripPickerProps {
  trips: { id: number; name: string }[];
  value: number | null;
  onChange: (id: number | null) => void;
}

function TripPicker({ trips, value, onChange }: TripPickerProps) {
  return (
    <FormControl fullWidth sx={{ mb: 2 }}>
      <InputLabel id="add-trip-picker-label">Trip</InputLabel>
      <Select
        labelId="add-trip-picker-label"
        label="Trip"
        value={value == null ? '' : String(value)}
        onChange={(e) => onChange(e.target.value === '' ? null : Number(e.target.value))}
      >
        {trips.map((t) => (
          <MenuItem key={t.id} value={String(t.id)}>
            {t.name}
          </MenuItem>
        ))}
      </Select>
    </FormControl>
  );
}

// ---------------------------------------------------------------------------
// Manual tab — per-type form building a one-part CreatePlanInput.
// ---------------------------------------------------------------------------

interface ManualTabProps {
  disabled: boolean;
  onCreate: (input: CreatePlanInput) => void;
}

function ManualTab({ disabled, onCreate }: ManualTabProps) {
  const [type, setType] = useState<PlanType>('flight');
  const [title, setTitle] = useState('');
  const [confRef, setConfRef] = useState('');
  const [notes, setNotes] = useState('');
  const [startLabel, setStartLabel] = useState('');
  const [endLabel, setEndLabel] = useState('');
  const [startsAt, setStartsAt] = useState<Date | null>(() => defaultStart());
  const [endsAt, setEndsAt] = useState<Date | null>(null);

  // Flight uses the existing lookup affordance (ident + date) per PRD §6.3.
  const [ident, setIdent] = useState('');

  const canSubmit = title.trim() !== '' && startsAt !== null && !disabled;

  const submit = () => {
    if (!canSubmit || startsAt == null) return;
    const part: PlanPartInput = {
      type,
      starts_at: startsAt.toISOString(),
      ends_at: endsAt ? endsAt.toISOString() : undefined,
      start_label: startLabel.trim() || undefined,
      end_label: endLabel.trim() || undefined,
    };
    if (type === 'flight' && ident.trim()) {
      part.flight = { ident: ident.trim().toUpperCase() };
    }
    const input: CreatePlanInput = {
      type,
      title: title.trim(),
      confirmation_ref: confRef.trim() || undefined,
      notes: notes.trim() || undefined,
      parts: [part],
    };
    onCreate(input);
  };

  const isFlight = type === 'flight';
  // Hotels span nights, so they always show an end ("check-out").
  const showEnd = type === 'hotel' || type === 'flight' || type === 'train' || type === 'ground';

  return (
    <Stack spacing={2} sx={{ pt: 1 }}>
      <FormControl fullWidth>
        <InputLabel id="manual-type-label">Type</InputLabel>
        <Select
          labelId="manual-type-label"
          label="Type"
          value={type}
          onChange={(e) => setType(e.target.value as PlanType)}
        >
          {PLAN_TYPES.map((t) => (
            <MenuItem key={t} value={t}>
              <Stack direction="row" spacing={1} alignItems="center">
                <PlanTypeIcon type={t} fontSize="small" />
                <span>{planTypeLabel(t)}</span>
              </Stack>
            </MenuItem>
          ))}
        </Select>
      </FormControl>

      <TextField
        label="Title"
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        required
        placeholder={isFlight ? 'e.g. BA286 to Lisbon' : `e.g. ${placeholderFor(type)}`}
        fullWidth
      />

      {isFlight && (
        <TextField
          label="Flight number (optional)"
          value={ident}
          onChange={(e) => setIdent(e.target.value)}
          placeholder="e.g. BA286"
          inputProps={{ style: { textTransform: 'uppercase' } }}
          helperText="Schedule and airports are looked up from the flight database when available."
          fullWidth
        />
      )}

      <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
        <TextField
          label={startFieldLabel(type)}
          value={startLabel}
          onChange={(e) => setStartLabel(e.target.value)}
          fullWidth
        />
        {showEnd && (
          <TextField
            label={endFieldLabel(type)}
            value={endLabel}
            onChange={(e) => setEndLabel(e.target.value)}
            fullWidth
          />
        )}
      </Stack>

      <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
        <DateTimePicker
          label={startTimeLabel(type)}
          value={startsAt}
          onChange={setStartsAt}
          ampm={false}
          sx={{ flexGrow: 1 }}
        />
        {showEnd && (
          <DateTimePicker
            label={endTimeLabel(type)}
            value={endsAt}
            onChange={setEndsAt}
            ampm={false}
            sx={{ flexGrow: 1 }}
          />
        )}
      </Stack>

      <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
        <TextField
          label="Confirmation ref (optional)"
          value={confRef}
          onChange={(e) => setConfRef(e.target.value)}
          fullWidth
        />
      </Stack>

      <TextField
        label="Notes (optional)"
        value={notes}
        onChange={(e) => setNotes(e.target.value)}
        multiline
        rows={2}
        fullWidth
      />

      <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
        <Button variant="contained" onClick={submit} disabled={!canSubmit}>
          Add to trip
        </Button>
      </Box>
    </Stack>
  );
}

// ---------------------------------------------------------------------------
// Paste / Upload tabs — collect text, hand it to ingest().
// ---------------------------------------------------------------------------

interface IngestTabProps {
  disabled: boolean;
  onIngest: (text: string) => void;
}

function PasteTab({ disabled, onIngest }: IngestTabProps) {
  const [text, setText] = useState('');
  return (
    <Stack spacing={2} sx={{ pt: 1 }}>
      <Typography variant="body2" color="text.secondary">
        Paste any confirmation text — a forwarded itinerary, a hotel email body,
        the taxi firm’s reply — and Aerly will extract the plan for you to
        confirm.
      </Typography>
      <TextField
        label="Confirmation text"
        value={text}
        onChange={(e) => setText(e.target.value)}
        multiline
        rows={8}
        fullWidth
        placeholder="Paste here…"
      />
      <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
        <Button
          variant="contained"
          onClick={() => onIngest(text)}
          disabled={disabled || text.trim() === ''}
        >
          Extract plan
        </Button>
      </Box>
    </Stack>
  );
}

function UploadTab({ disabled, onIngest }: IngestTabProps) {
  const [fileName, setFileName] = useState('');
  const [text, setText] = useState('');

  const onFile = async (file: File | undefined) => {
    if (!file) return;
    setFileName(file.name);
    // Text-ish documents (e.g. .txt/.eml) can be read inline and sent through
    // the same text path. Binary tickets (PDF) are handled by the backend's
    // document extractor; until the upload transport lands we surface the
    // filename and let the user paste, so the flow degrades gracefully.
    if (/text|message|json/.test(file.type) || /\.(txt|eml|md)$/i.test(file.name)) {
      try {
        setText(await file.text());
      } catch {
        setText('');
      }
    } else {
      setText('');
    }
  };

  return (
    <Stack spacing={2} sx={{ pt: 1 }}>
      <Typography variant="body2" color="text.secondary">
        Drop in a ticket or confirmation (PDF, email, or text) and Aerly will
        extract the plan for you to confirm.
      </Typography>
      <Button variant="outlined" component="label">
        Choose file
        <input
          type="file"
          hidden
          accept=".pdf,.txt,.eml,.md,application/pdf,text/plain,message/rfc822"
          onChange={(e) => void onFile(e.target.files?.[0])}
        />
      </Button>
      {fileName && (
        <Typography variant="caption" color="text.secondary">
          Selected: {fileName}
        </Typography>
      )}
      <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
        <Button
          variant="contained"
          onClick={() => onIngest(text)}
          disabled={disabled || (text.trim() === '' && fileName === '')}
        >
          Extract plan
        </Button>
      </Box>
    </Stack>
  );
}

// ---------------------------------------------------------------------------
// From email tab — show the forwarding address the backend exposes.
// ---------------------------------------------------------------------------

function EmailTab({ enabled, address }: { enabled: boolean; address?: string }) {
  if (!enabled || !address) {
    return (
      <Alert severity="info" variant="outlined" sx={{ mt: 1 }}>
        Email forwarding isn’t enabled on this server. Use Paste text or Upload
        to add a confirmation, or add the plan manually.
      </Alert>
    );
  }
  return (
    <Stack spacing={2} sx={{ pt: 1 }}>
      <Typography variant="body2" color="text.secondary">
        Forward any booking confirmation to the address below and Aerly will
        extract the plan and file it into the trip whose dates best fit — you
        can move it afterwards if the guess is off.
      </Typography>
      <Alert severity="info" variant="outlined">
        <Stack spacing={0.5}>
          <Typography variant="body2">Forward confirmations to:</Typography>
          <Link href={`mailto:${address}`} sx={{ fontWeight: 600, wordBreak: 'break-all' }}>
            {address}
          </Link>
        </Stack>
      </Alert>
    </Stack>
  );
}

// ---------------------------------------------------------------------------
// Confirm step — edit + accept the proposed plans.
// ---------------------------------------------------------------------------

interface ConfirmStepProps {
  proposals: ProposedPlan[];
  onCancel: () => void;
  onConfirm: (plans: ConfirmPlanInput[]) => void;
  busy: boolean;
}

/** Per-proposal editable state in the confirm step. `accepted` toggles whether
 * a proposal is committed; `applySupersede` chooses, for a proposed
 * supersession, whether to replace the matched existing part or add alongside. */
interface DraftPlan {
  type: PlanType;
  title: string;
  confirmation_ref: string;
  notes: string;
  confidence: number;
  parts: PlanPartInput[];
  supersedes_part_id?: number;
  accepted: boolean;
  applySupersede: boolean;
}

function toDraft(p: ProposedPlan): DraftPlan {
  return {
    type: p.type,
    title: p.title,
    confirmation_ref: p.confirmation_ref,
    notes: p.notes,
    confidence: p.confidence,
    parts: p.parts.map((part) => ({
      type: part.type,
      seq: part.seq,
      starts_at: part.starts_at,
      ends_at: part.ends_at,
      start_tz: part.start_tz || undefined,
      end_tz: part.end_tz || undefined,
      start_label: part.start_label || undefined,
      start_lat: part.start_lat,
      start_lon: part.start_lon,
      end_label: part.end_label || undefined,
      end_lat: part.end_lat,
      end_lon: part.end_lon,
      flight: part.flight,
      hotel: part.hotel,
      train: part.train,
      ground: part.ground,
      dining: part.dining,
      excursion: part.excursion,
    })),
    supersedes_part_id: p.supersedes_part_id,
    accepted: true,
    applySupersede: p.supersedes_part_id != null,
  };
}

function ConfirmStep({ proposals, onCancel, onConfirm, busy }: ConfirmStepProps) {
  const [drafts, setDrafts] = useState<DraftPlan[]>(() => proposals.map(toDraft));

  const update = (idx: number, patch: Partial<DraftPlan>) => {
    setDrafts((ds) => ds.map((d, i) => (i === idx ? { ...d, ...patch } : d)));
  };

  const acceptedCount = useMemo(() => drafts.filter((d) => d.accepted).length, [drafts]);

  const confirm = () => {
    const plans: ConfirmPlanInput[] = drafts
      .filter((d) => d.accepted)
      .map((d) => ({
        type: d.type,
        title: d.title.trim(),
        confirmation_ref: d.confirmation_ref.trim() || undefined,
        notes: d.notes.trim() || undefined,
        parts: d.parts,
        // Only carry the supersession when the user kept "replace existing".
        supersedes_part_id: d.applySupersede ? d.supersedes_part_id : undefined,
      }));
    onConfirm(plans);
  };

  if (proposals.length === 0) {
    return (
      <Stack spacing={2}>
        <Alert severity="warning" variant="outlined">
          Aerly couldn’t find any plans in that. Try a different paste or upload,
          or add the plan manually.
        </Alert>
        <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
          <Button onClick={onCancel}>Back</Button>
        </Box>
      </Stack>
    );
  }

  return (
    <Stack spacing={2}>
      <Typography variant="body2" color="text.secondary">
        Review what Aerly extracted. Edit anything that’s off, then add the plans
        you want. Items flagged as uncertain are worth a second look.
      </Typography>

      {drafts.map((d, idx) => (
        <Box
          key={idx}
          data-testid={`proposal-${idx}`}
          sx={{
            border: 1,
            borderColor: d.accepted ? 'divider' : 'action.disabledBackground',
            borderRadius: 1,
            p: 2,
            opacity: d.accepted ? 1 : 0.6,
          }}
        >
          <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 1 }}>
            <PlanTypeIcon type={d.type} fontSize="small" />
            <Typography variant="subtitle2" sx={{ flexGrow: 1 }}>
              {planTypeLabel(d.type)}
              {d.parts.length > 1 ? ` · ${d.parts.length} parts` : ''}
            </Typography>
            {d.confidence < LOW_CONFIDENCE && (
              <Chip
                label="Low confidence — please check"
                size="small"
                color="warning"
                variant="outlined"
              />
            )}
          </Stack>

          {d.supersedes_part_id != null && (
            <Alert severity="warning" variant="outlined" sx={{ mb: 1.5 }}>
              <Stack spacing={1}>
                <Typography variant="body2">
                  This looks like a rebooking that replaces an existing plan part
                  (#{d.supersedes_part_id}).
                </Typography>
                <FormControl size="small">
                  <Select
                    value={d.applySupersede ? 'replace' : 'keep'}
                    onChange={(e) =>
                      update(idx, { applySupersede: e.target.value === 'replace' })
                    }
                    aria-label="Supersession choice"
                  >
                    <MenuItem value="replace">Replace the existing part</MenuItem>
                    <MenuItem value="keep">Add as a new part, keep the existing</MenuItem>
                  </Select>
                </FormControl>
              </Stack>
            </Alert>
          )}

          <Stack spacing={1.5}>
            <TextField
              label="Title"
              value={d.title}
              onChange={(e) => update(idx, { title: e.target.value })}
              size="small"
              fullWidth
              disabled={!d.accepted}
            />
            <TextField
              label="Confirmation ref"
              value={d.confirmation_ref}
              onChange={(e) => update(idx, { confirmation_ref: e.target.value })}
              size="small"
              fullWidth
              disabled={!d.accepted}
            />
            <TextField
              label="Notes"
              value={d.notes}
              onChange={(e) => update(idx, { notes: e.target.value })}
              size="small"
              fullWidth
              multiline
              disabled={!d.accepted}
            />
            {d.parts.map((part, pIdx) => (
              <Box key={pIdx} sx={{ pl: 1, borderLeft: 2, borderColor: 'divider' }}>
                <Typography variant="caption" color="text.secondary">
                  {(part.start_label || planTypeLabel(part.type)) +
                    (part.end_label ? ` → ${part.end_label}` : '')}
                  {part.starts_at ? ` · ${fmtIso(part.starts_at)}` : ''}
                </Typography>
              </Box>
            ))}
          </Stack>

          <Divider sx={{ my: 1.5 }} />
          <Button
            size="small"
            onClick={() => update(idx, { accepted: !d.accepted })}
            color={d.accepted ? 'inherit' : 'primary'}
          >
            {d.accepted ? 'Skip this one' : 'Include this one'}
          </Button>
        </Box>
      ))}

      <Box sx={{ display: 'flex', justifyContent: 'flex-end', gap: 1 }}>
        <Button onClick={onCancel} disabled={busy}>
          Back
        </Button>
        <Button
          variant="contained"
          onClick={confirm}
          disabled={busy || acceptedCount === 0}
        >
          {acceptedCount > 1 ? `Add ${acceptedCount} plans` : 'Add to trip'}
        </Button>
      </Box>
    </Stack>
  );
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

function defaultStart(): Date {
  const d = new Date();
  d.setHours(d.getHours() + 1, 0, 0, 0);
  return d;
}

function fmtIso(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function placeholderFor(type: PlanType): string {
  switch (type) {
    case 'hotel':
      return 'Hotel Lisboa';
    case 'train':
      return 'Eurostar to Paris';
    case 'ground':
      return 'Airport transfer';
    case 'dining':
      return 'Dinner at Belcanto';
    case 'excursion':
      return 'Walking tour';
    default:
      return planTypeLabel(type);
  }
}

function startFieldLabel(type: PlanType): string {
  switch (type) {
    case 'flight':
    case 'train':
    case 'ground':
      return 'From';
    case 'hotel':
      return 'Property';
    default:
      return 'Location';
  }
}

function endFieldLabel(type: PlanType): string {
  switch (type) {
    case 'hotel':
      return 'Room / details';
    default:
      return 'To';
  }
}

function startTimeLabel(type: PlanType): string {
  switch (type) {
    case 'hotel':
      return 'Check-in';
    case 'dining':
    case 'excursion':
      return 'Time';
    default:
      return 'Departs';
  }
}

function endTimeLabel(type: PlanType): string {
  switch (type) {
    case 'hotel':
      return 'Check-out';
    default:
      return 'Arrives';
  }
}

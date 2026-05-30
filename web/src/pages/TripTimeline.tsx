import { useMemo, useState } from 'react';
import { Link as RouterLink } from 'react-router-dom';
import {
  Box,
  Card,
  Chip,
  Dialog,
  DialogContent,
  DialogTitle,
  Link,
  Stack,
  Typography,
} from '@mui/material';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';

import { useStore } from '../state/store';
import type { Plan, PlanPart } from '../api/types';
import PlanTypeIcon from '../components/PlanTypeIcon';
import {
  buildTimeline,
  fmtPartTimeRange,
  hotelNights,
  isHotelBand,
  planTypeLabel,
} from '../lib/trip-format';

// Accent palette used to visually tie a plan's parts together (PRD §6.2). A
// plan's parts all share the same accent stripe and connector, so a return
// flight's two legs read as one booking even days apart. Colours are assigned
// by stable order of plan id so the same plan keeps its colour across renders.
const ACCENTS = ['#1f5fa8', '#d97706', '#2e7d32', '#7b1fa2', '#c2185b', '#00838f', '#5d4037'];

function accentFor(planIds: number[], planId: number): string {
  const idx = planIds.indexOf(planId);
  return ACCENTS[(idx < 0 ? 0 : idx) % ACCENTS.length];
}

/** Default trip detail view (spec §11, PRD §6.2): a day-grouped vertical list
 * of plan parts sorted by `effective_at`, with sticky local-day headers, the
 * right MUI icon per type, local-time ranges, parts of one plan visually tied
 * together, multi-night hotels as a band, and superseded parts greyed. */
export default function TripTimeline() {
  const currentTrip = useStore((s) => s.currentTrip);
  const plans = useMemo(() => currentTrip?.plans ?? [], [currentTrip]);

  const days = useMemo(() => buildTimeline(plans), [plans]);
  // Stable plan ordering for accent assignment (first appearance on timeline).
  const planIds = useMemo(() => {
    const seen: number[] = [];
    for (const d of days) {
      for (const { plan } of d.parts) if (!seen.includes(plan.id)) seen.push(plan.id);
    }
    return seen;
  }, [days]);

  // Which plan ids span more than one timeline part — only those get the
  // "part of a multi-part booking" connector treatment.
  const multiPartPlanIds = useMemo(() => {
    const counts = new Map<number, number>();
    for (const d of days) for (const { plan } of d.parts) counts.set(plan.id, (counts.get(plan.id) ?? 0) + 1);
    return new Set([...counts].filter(([, n]) => n > 1).map(([id]) => id));
  }, [days]);

  const [planDetail, setPlanDetail] = useState<Plan | null>(null);

  if (!currentTrip) {
    return (
      <Box sx={{ p: 3 }}>
        <Typography color="text.secondary">Loading…</Typography>
      </Box>
    );
  }

  if (days.length === 0) {
    return (
      <Box sx={{ p: 3 }}>
        <Typography color="text.secondary">
          Nothing on this trip yet. Use <strong>Add to trip</strong> to add a flight, hotel, or
          other plan.
        </Typography>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3, maxWidth: 760, mx: 'auto' }}>
      {days.map((day) => (
        <Box key={day.dayKey} sx={{ mb: 2 }}>
          <Typography
            variant="subtitle2"
            color="text.secondary"
            sx={{
              position: 'sticky',
              top: 0,
              zIndex: 1,
              py: 0.75,
              bgcolor: 'background.default',
              borderBottom: 1,
              borderColor: 'divider',
            }}
          >
            {day.label}
          </Typography>
          <Stack spacing={1.5} sx={{ mt: 1.5 }}>
            {day.parts.map(({ part, plan }) => (
              <PartCard
                key={part.id}
                part={part}
                plan={plan}
                accent={accentFor(planIds, plan.id)}
                multiPart={multiPartPlanIds.has(plan.id)}
                onOpenPlan={() => setPlanDetail(plan)}
              />
            ))}
          </Stack>
        </Box>
      ))}

      <PlanDetailDialog plan={planDetail} onClose={() => setPlanDetail(null)} />
    </Box>
  );
}

interface PartCardProps {
  part: PlanPart;
  plan: Plan;
  accent: string;
  multiPart: boolean;
  onOpenPlan: () => void;
}

function PartCard({ part, plan, accent, multiPart, onOpenPlan }: PartCardProps) {
  // A cancelled part stays on the timeline, greyed out, until it's tidied
  // away (PRD §6.2/§6.9). On a rebooking the OLD part is the one stamped
  // `status='cancelled'` and marked superseded — the NEW part carries
  // `supersedes_id` and stays full-colour. So we key the greying purely on
  // `status === 'cancelled'`, which also correctly greys a plain cancellation.
  // Dismissed parts are already dropped by buildTimeline().
  const greyed = part.status === 'cancelled';
  const band = isHotelBand(part);

  return (
    <Card
      variant="outlined"
      onClick={onOpenPlan}
      sx={{
        position: 'relative',
        cursor: 'pointer',
        opacity: greyed ? 0.55 : 1,
        borderLeft: `4px solid ${accent}`,
        // The hotel band reads as a continuous strip across its nights.
        ...(band ? { bgcolor: 'action.hover' } : {}),
        '&:hover': { boxShadow: 1 },
      }}
      data-testid={`part-card-${part.id}`}
    >
      <Stack direction="row" spacing={1.5} sx={{ p: 1.5 }} alignItems="flex-start">
        <PlanTypeIcon type={part.type} sx={{ color: accent, mt: 0.25 }} />
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Stack direction="row" alignItems="center" spacing={1}>
            <Typography variant="subtitle2" sx={{ fontWeight: 600 }} noWrap>
              {plan.title || planTypeLabel(part.type)}
            </Typography>
            {multiPart && (
              <Chip
                label="multi-part"
                size="small"
                variant="outlined"
                sx={{ height: 18, fontSize: 10, borderColor: accent, color: accent }}
              />
            )}
            {part.status === 'confirmed' && (
              <Chip label="confirmed" size="small" color="success" variant="outlined" sx={{ height: 18, fontSize: 10 }} />
            )}
            {greyed && (
              <Chip label="cancelled" size="small" color="warning" variant="outlined" sx={{ height: 18, fontSize: 10 }} />
            )}
          </Stack>

          <Typography variant="body2" color="text.secondary" noWrap>
            {part.start_label}
            {part.end_label ? ` → ${part.end_label}` : ''}
          </Typography>

          <Typography variant="caption" color="text.secondary">
            {band ? `${hotelNights(part)} night${hotelNights(part) === 1 ? '' : 's'}` : fmtPartTimeRange(part)}
          </Typography>

          {plan.confirmation_ref && (
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
              Ref: {plan.confirmation_ref}
            </Typography>
          )}

          {part.type === 'flight' && part.flight && (
            <Link
              component={RouterLink}
              to={`/tracker?part=${part.id}`}
              onClick={(e) => e.stopPropagation()}
              variant="caption"
              sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5, mt: 0.25 }}
            >
              Track {part.flight.ident || plan.title} <OpenInNewIcon sx={{ fontSize: 12 }} />
            </Link>
          )}
        </Box>
      </Stack>
    </Card>
  );
}

/** Tap-through detail: lists the whole plan and all its parts so a user who
 * tapped any one part sees the entire booking (PRD §6.2). */
function PlanDetailDialog({ plan, onClose }: { plan: Plan | null; onClose: () => void }) {
  if (!plan) return null;
  const parts = [...plan.parts].sort(
    (a, b) => new Date(a.effective_at ?? a.starts_at).getTime() - new Date(b.effective_at ?? b.starts_at).getTime(),
  );
  return (
    <Dialog open={plan !== null} onClose={onClose} fullWidth maxWidth="xs">
      <DialogTitle>
        <Stack direction="row" alignItems="center" spacing={1}>
          <PlanTypeIcon type={plan.type} fontSize="small" />
          <span>{plan.title || planTypeLabel(plan.type)}</span>
        </Stack>
      </DialogTitle>
      <DialogContent>
        {plan.confirmation_ref && (
          <Typography variant="body2" color="text.secondary" gutterBottom>
            Confirmation: {plan.confirmation_ref}
          </Typography>
        )}
        <Stack spacing={1.5} sx={{ mt: 1 }}>
          {parts.map((part) => (
            <Box key={part.id} sx={{ opacity: part.dismissed_at ? 0.5 : 1 }}>
              <Typography variant="subtitle2">
                {part.start_label}
                {part.end_label ? ` → ${part.end_label}` : ''}
              </Typography>
              <Typography variant="caption" color="text.secondary">
                {fmtPartTimeRange(part)}
              </Typography>
            </Box>
          ))}
        </Stack>
        {plan.notes && (
          <Typography variant="body2" color="text.secondary" sx={{ mt: 2, whiteSpace: 'pre-wrap' }}>
            {plan.notes}
          </Typography>
        )}
      </DialogContent>
    </Dialog>
  );
}

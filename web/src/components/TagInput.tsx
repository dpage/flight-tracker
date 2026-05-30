import { useEffect, useMemo, useState } from 'react';
import { Autocomplete, Chip, TextField } from '@mui/material';

import { useStore } from '../state/store';

/** Normalise a label the way the backend does: trimmed, lower-cased. Keeps the
 * de-dupe in the combo consistent with what `setTripTags` will store. */
function normalize(label: string): string {
  return label.trim().toLowerCase();
}

interface TagInputProps {
  /** Current labels on the trip. */
  value: string[];
  /** Called with the full new label list whenever the set changes. The caller
   * persists it (typically via `setTripTags`). */
  onChange: (labels: string[]) => void;
  disabled?: boolean;
  label?: string;
  helperText?: string;
}

/** Reusable tag combo for editing a trip's shared labels (PRD §6.6).
 *
 * Free-solo multi-select: the user can type a brand-new tag (creating it is just
 * typing it) or pick from `suggestTags` autocompletes. Suggestions are visibility
 * gated server-side — they only ever surface tags on trips the viewer can already
 * see, which is what keeps "tags group, they never grant." The component never
 * fetches a trip; it only proposes labels. */
export default function TagInput({
  value,
  onChange,
  disabled,
  label = 'Tags',
  helperText,
}: TagInputProps) {
  const suggestTags = useStore((s) => s.suggestTags);
  const suggestions = useStore((s) => s.tagSuggestions);
  const [input, setInput] = useState('');

  // Debounced suggest as the user types. Empty input still asks for the
  // recent/popular set the backend returns for an empty query.
  useEffect(() => {
    const q = input.trim();
    const t = setTimeout(() => {
      void suggestTags(q);
    }, 200);
    return () => clearTimeout(t);
  }, [input, suggestTags]);

  // Option list = server suggestions minus already-selected labels, so the
  // combo never re-offers a tag the trip already has.
  const options = useMemo(() => {
    const have = new Set(value.map(normalize));
    const seen = new Set<string>();
    const out: string[] = [];
    for (const s of suggestions) {
      const n = normalize(s.label);
      if (!n || have.has(n) || seen.has(n)) continue;
      seen.add(n);
      out.push(s.label);
    }
    return out;
  }, [suggestions, value]);

  const commit = (raw: string[]) => {
    // De-dupe (case-insensitively) and drop blanks, preserving first spelling.
    const seen = new Set<string>();
    const next: string[] = [];
    for (const r of raw) {
      const n = normalize(r);
      if (!n || seen.has(n)) continue;
      seen.add(n);
      next.push(n);
    }
    onChange(next);
  };

  return (
    <Autocomplete
      multiple
      freeSolo
      disabled={disabled}
      options={options}
      value={value}
      inputValue={input}
      onInputChange={(_, v) => setInput(v)}
      onChange={(_, v) => {
        commit(v as string[]);
        setInput('');
      }}
      filterOptions={(opts) => opts}
      renderTags={(tags, getTagProps) =>
        tags.map((t, i) => (
          <Chip {...getTagProps({ index: i })} key={t} label={t} size="small" />
        ))
      }
      renderInput={(params) => (
        <TextField
          {...params}
          label={label}
          placeholder="Add a tag…"
          helperText={helperText ?? 'Type to create or match a shared tag, e.g. pgconf-eu-26.'}
        />
      )}
    />
  );
}

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { TagSuggestion } from '../api/types';

const suggestTags = vi.fn();

const state = {
  suggestTags,
  tagSuggestions: [] as TagSuggestion[],
};

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof state) => unknown) => sel(state),
}));

import TagInput from './TagInput';

beforeEach(() => {
  vi.clearAllMocks();
  state.tagSuggestions = [];
});

describe('TagInput', () => {
  it('renders the current tags as chips', () => {
    render(<TagInput value={['pgconf-eu-26', 'family']} onChange={vi.fn()} />);
    expect(screen.getByText('pgconf-eu-26')).toBeInTheDocument();
    expect(screen.getByText('family')).toBeInTheDocument();
  });

  it('calls suggestTags (debounced) as the user types', async () => {
    const user = userEvent.setup();
    render(<TagInput value={[]} onChange={vi.fn()} />);
    // Initial empty-query suggest fires on mount.
    await waitFor(() => expect(suggestTags).toHaveBeenCalledWith(''));
    await user.type(screen.getByRole('combobox'), 'pg');
    await waitFor(() => expect(suggestTags).toHaveBeenCalledWith('pg'));
  });

  it('adds a free-typed tag, normalised to lower-case', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<TagInput value={[]} onChange={onChange} />);
    await user.type(screen.getByRole('combobox'), 'PGConf-EU-26{enter}');
    expect(onChange).toHaveBeenCalledWith(['pgconf-eu-26']);
  });

  it('offers server suggestions but hides already-selected tags', async () => {
    const user = userEvent.setup();
    state.tagSuggestions = [{ label: 'beach' }, { label: 'family' }];
    render(<TagInput value={['family']} onChange={vi.fn()} />);
    await user.click(screen.getByRole('combobox'));
    // 'beach' offered; 'family' (already selected) is not re-offered as an option.
    await waitFor(() => expect(screen.getByText('beach')).toBeInTheDocument());
    const options = screen.queryAllByRole('option').map((o) => o.textContent);
    expect(options).toContain('beach');
    expect(options).not.toContain('family');
  });

  it('de-dupes case-insensitively when committing', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<TagInput value={['family']} onChange={onChange} />);
    await user.type(screen.getByRole('combobox'), 'FAMILY{enter}');
    // Already present (case-insensitively) so the set is unchanged.
    expect(onChange).toHaveBeenCalledWith(['family']);
  });
});

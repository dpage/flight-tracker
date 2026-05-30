import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { CalendarToken } from '../api/types';

const h = vi.hoisted(() => ({
  api: {
    listCalendarTokens: vi.fn(),
    issueCalendarToken: vi.fn(),
    revokeCalendarToken: vi.fn(),
  },
}));

vi.mock('../api/client', () => ({ api: h.api }));

import CalendarSubscribeDialog from './CalendarSubscribeDialog';

function token(over: Partial<CalendarToken> = {}): CalendarToken {
  return {
    scope: 'me',
    token: 'tok-abc',
    url: 'https://aerly.test/cal/tok-abc.ics',
    created_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

const writeText = vi.fn().mockResolvedValue(undefined);

beforeEach(() => {
  vi.resetAllMocks();
  h.api.listCalendarTokens.mockResolvedValue([]);
  writeText.mockResolvedValue(undefined);
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText },
  });
});

describe('CalendarSubscribeDialog', () => {
  it('lists tokens on open and shows the matching feed URL', async () => {
    h.api.listCalendarTokens.mockResolvedValue([token()]);
    render(<CalendarSubscribeDialog open scope="me" onClose={vi.fn()} />);
    await waitFor(() => expect(h.api.listCalendarTokens).toHaveBeenCalled());
    const field = await screen.findByLabelText('Feed URL');
    expect(field).toHaveValue('https://aerly.test/cal/tok-abc.ics');
  });

  it('offers to create a feed link when none exists, and issues one', async () => {
    const user = userEvent.setup();
    h.api.listCalendarTokens.mockResolvedValue([]);
    h.api.issueCalendarToken.mockResolvedValue(token());
    render(<CalendarSubscribeDialog open scope="me" onClose={vi.fn()} />);
    const createBtn = await screen.findByRole('button', { name: /create feed link/i });
    await user.click(createBtn);
    expect(h.api.issueCalendarToken).toHaveBeenCalledWith('me', undefined);
    expect(await screen.findByLabelText('Feed URL')).toHaveValue(token().url);
  });

  it('passes the id for trip scope when issuing', async () => {
    const user = userEvent.setup();
    h.api.listCalendarTokens.mockResolvedValue([]);
    h.api.issueCalendarToken.mockResolvedValue(token({ scope: 'trip', url: 'https://x/trip.ics' }));
    render(<CalendarSubscribeDialog open scope="trip" id={42} onClose={vi.fn()} />);
    await user.click(await screen.findByRole('button', { name: /create feed link/i }));
    expect(h.api.issueCalendarToken).toHaveBeenCalledWith('trip', 42);
  });

  it('copies the URL to the clipboard', async () => {
    // userEvent.setup() installs its own clipboard stub, so pin ours afterwards.
    const user = userEvent.setup();
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
    h.api.listCalendarTokens.mockResolvedValue([token()]);
    render(<CalendarSubscribeDialog open scope="me" onClose={vi.fn()} />);
    await screen.findByLabelText('Feed URL');
    await user.click(screen.getByRole('button', { name: /copy feed url/i }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(token().url));
  });

  it('regenerate revokes the old token then issues a fresh one', async () => {
    const user = userEvent.setup();
    h.api.listCalendarTokens.mockResolvedValue([token()]);
    h.api.revokeCalendarToken.mockResolvedValue(undefined);
    h.api.issueCalendarToken.mockResolvedValue(token({ token: 'tok-new', url: 'https://x/new.ics' }));
    render(<CalendarSubscribeDialog open scope="me" onClose={vi.fn()} />);
    await screen.findByLabelText('Feed URL');
    await user.click(screen.getByRole('button', { name: /regenerate link/i }));
    await waitFor(() => expect(h.api.revokeCalendarToken).toHaveBeenCalledWith('tok-abc'));
    expect(h.api.issueCalendarToken).toHaveBeenCalledWith('me', undefined);
    await waitFor(() =>
      expect(screen.getByLabelText('Feed URL')).toHaveValue('https://x/new.ics'),
    );
  });

  it('surfaces an error when listing fails', async () => {
    h.api.listCalendarTokens.mockRejectedValue(new Error('boom'));
    render(<CalendarSubscribeDialog open scope="me" onClose={vi.fn()} />);
    expect(await screen.findByText('boom')).toBeInTheDocument();
  });
});

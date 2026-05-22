import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { UserEmail } from '../api/types';

const h = vi.hoisted(() => ({
  api: {
    listMyEmails: vi.fn(),
    addMyEmail: vi.fn(),
    resendMyEmail: vi.fn(),
    deleteMyEmail: vi.fn(),
  },
  setError: vi.fn(),
}));

vi.mock('../api/client', () => ({ api: h.api }));
vi.mock('../state/store', () => ({
  useStore: (sel: (s: { setError: (m: string | null) => void }) => unknown) =>
    sel({ setError: h.setError }),
}));

import EmailsDialog from './EmailsDialog';

function row(over: Partial<UserEmail> = {}): UserEmail {
  return {
    id: 1,
    address: 'alice@example.com',
    verified: true,
    created_at: new Date().toISOString(),
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  h.api.listMyEmails.mockResolvedValue([]);
});

describe('EmailsDialog', () => {
  it("lists the user's emails when opened", async () => {
    h.api.listMyEmails.mockResolvedValue([
      row({ id: 1, address: 'alice@example.com', verified: true }),
      row({ id: 2, address: 'alice+2@example.com', verified: false }),
    ]);
    render(<EmailsDialog open onClose={() => {}} />);
    await screen.findByText('alice@example.com');
    expect(screen.getByText('alice+2@example.com')).toBeInTheDocument();
    expect(screen.getByText(/verified/i)).toBeInTheDocument();
    expect(screen.getByText(/pending/i)).toBeInTheDocument();
  });

  it('adds an email and appends the row', async () => {
    h.api.listMyEmails.mockResolvedValue([]);
    h.api.addMyEmail.mockResolvedValue(
      row({ id: 9, address: 'new@example.com', verified: false }),
    );
    render(<EmailsDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.api.listMyEmails).toHaveBeenCalled());

    const field = screen.getByLabelText(/email address/i);
    await userEvent.type(field, 'new@example.com');
    await userEvent.click(screen.getByRole('button', { name: /^add$/i }));

    await screen.findByText('new@example.com');
    expect(h.api.addMyEmail).toHaveBeenCalledWith('new@example.com');
    expect((field as HTMLInputElement).value).toBe('');
  });

  it('deletes an email after confirm()', async () => {
    h.api.listMyEmails.mockResolvedValue([
      row({ id: 5, address: 'gone@example.com', verified: true }),
    ]);
    h.api.deleteMyEmail.mockResolvedValue();
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    render(<EmailsDialog open onClose={() => {}} />);
    await screen.findByText('gone@example.com');

    await userEvent.click(screen.getByRole('button', { name: /delete gone@example.com/i }));
    await waitFor(() => expect(h.api.deleteMyEmail).toHaveBeenCalledWith(5));
    expect(screen.queryByText('gone@example.com')).not.toBeInTheDocument();
    confirmSpy.mockRestore();
  });

  it('resends verification for a pending row', async () => {
    h.api.listMyEmails.mockResolvedValue([
      row({ id: 7, address: 'pending@example.com', verified: false }),
    ]);
    h.api.resendMyEmail.mockResolvedValue(
      row({ id: 7, address: 'pending@example.com', verified: false }),
    );
    render(<EmailsDialog open onClose={() => {}} />);
    await screen.findByText('pending@example.com');

    await userEvent.click(screen.getByRole('button', { name: /resend pending@example.com/i }));
    await waitFor(() => expect(h.api.resendMyEmail).toHaveBeenCalledWith(7));
  });

  it('surfaces server errors via setError', async () => {
    h.api.listMyEmails.mockResolvedValue([]);
    h.api.addMyEmail.mockRejectedValue(new Error('address already registered'));
    render(<EmailsDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.api.listMyEmails).toHaveBeenCalled());

    await userEvent.type(screen.getByLabelText(/email address/i), 'taken@example.com');
    await userEvent.click(screen.getByRole('button', { name: /^add$/i }));

    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('address already registered'));
  });

  it('surfaces delete errors via setError', async () => {
    h.api.listMyEmails.mockResolvedValue([
      row({ id: 5, address: 'gone@example.com', verified: true }),
    ]);
    h.api.deleteMyEmail.mockRejectedValue(new Error('delete boom'));
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    render(<EmailsDialog open onClose={() => {}} />);
    await screen.findByText('gone@example.com');

    await userEvent.click(screen.getByRole('button', { name: /delete gone@example.com/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('delete boom'));
    confirmSpy.mockRestore();
  });

  it('surfaces resend errors via setError', async () => {
    h.api.listMyEmails.mockResolvedValue([
      row({ id: 7, address: 'pending@example.com', verified: false }),
    ]);
    h.api.resendMyEmail.mockRejectedValue(new Error('mail down'));
    render(<EmailsDialog open onClose={() => {}} />);
    await screen.findByText('pending@example.com');

    await userEvent.click(screen.getByRole('button', { name: /resend pending@example.com/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('mail down'));
  });

  it('stringifies non-Error rejections', async () => {
    // Mocked rejection is a plain string (not an Error instance) — exercises
    // the String(err) branch of reportError.
    h.api.listMyEmails.mockRejectedValue('listing exploded');
    render(<EmailsDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('listing exploded'));
  });

  it('does not call deleteMyEmail when the user cancels confirm()', async () => {
    h.api.listMyEmails.mockResolvedValue([
      row({ id: 5, address: 'stays@example.com', verified: true }),
    ]);
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);
    render(<EmailsDialog open onClose={() => {}} />);
    await screen.findByText('stays@example.com');

    await userEvent.click(screen.getByRole('button', { name: /delete stays@example.com/i }));
    expect(h.api.deleteMyEmail).not.toHaveBeenCalled();
    expect(screen.getByText('stays@example.com')).toBeInTheDocument();
    confirmSpy.mockRestore();
  });
});

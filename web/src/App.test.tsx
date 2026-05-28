import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

const h = vi.hoisted(() => {
  return {
    connectSSE: vi.fn((_handlers: {
      onFlight: (f: unknown) => void;
      onDelete: (id: number) => void;
      onNotifications: (n: unknown) => void;
    }) => vi.fn()),
    api: {
      acceptFriendToken: vi.fn(),
    },
    state: {
      auth: 'loading' as 'loading' | 'anonymous' | 'authenticated',
      error: null as string | null,
      notice: null as { message: string; severity: 'success' | 'info' } | null,
      init: vi.fn(),
      setError: vi.fn(),
      setNotice: vi.fn(),
      refreshNotifications: vi.fn(),
      applyFlightUpdate: vi.fn(),
      applyFlightDelete: vi.fn(),
      applyNotificationsUpdate: vi.fn(),
      users: [] as Array<{ id: number; name: string }>,
    },
  };
});
const connectSSE = h.connectSSE;
const state = h.state;

vi.mock('./sse', () => ({ connectSSE: h.connectSSE }));
vi.mock('./api/client', () => ({ api: h.api, ApiError: class {} }));
vi.mock('./components/AppShell', () => ({ default: () => <div>APP_SHELL</div> }));
vi.mock('./components/Login', () => ({ default: () => <div>LOGIN</div> }));
vi.mock('./components/PrivacyPolicy', () => ({ default: () => <div>PRIVACY_POLICY</div> }));
vi.mock('./components/TermsOfService', () => ({ default: () => <div>TERMS_OF_SERVICE</div> }));
vi.mock('./state/store', () => ({
  useStore: (sel: (s: typeof h.state) => unknown) => sel(h.state),
}));

import App from './App';

beforeEach(() => {
  vi.clearAllMocks();
  state.auth = 'loading';
  state.error = null;
  state.notice = null;
  state.users = [];
  window.history.pushState({}, '', '/');
});

describe('App', () => {
  it('shows a spinner while loading and calls init', () => {
    state.auth = 'loading';
    render(<App />);
    expect(document.querySelector('.MuiCircularProgress-root')).toBeTruthy();
    expect(state.init).toHaveBeenCalled();
  });

  it('renders Login when anonymous', () => {
    state.auth = 'anonymous';
    render(<App />);
    expect(screen.getByText('LOGIN')).toBeInTheDocument();
    expect(connectSSE).not.toHaveBeenCalled();
  });

  it('renders AppShell and wires SSE when authenticated', () => {
    state.auth = 'authenticated';
    render(<App />);
    expect(screen.getByText('APP_SHELL')).toBeInTheDocument();
    expect(connectSSE).toHaveBeenCalledTimes(1);
    // The SSE handlers should forward to applyFlightUpdate / applyFlightDelete.
    const handlers = connectSSE.mock.calls[0][0];
    handlers.onFlight({ id: 7 });
    expect(state.applyFlightUpdate).toHaveBeenCalledWith({ id: 7 });
    handlers.onDelete(7);
    expect(state.applyFlightDelete).toHaveBeenCalledWith(7);
  });

  it('shows an error snackbar and clears it via the Alert close button', async () => {
    state.auth = 'anonymous';
    state.error = 'boom';
    render(<App />);
    expect(screen.getByText('boom')).toBeInTheDocument();
    const closeBtn = screen.getByRole('button', { name: /close/i });
    await userEvent.click(closeBtn);
    expect(state.setError).toHaveBeenCalledWith(null);
  });

  it('renders PrivacyPolicy at /privacy without waiting for auth', () => {
    window.history.pushState({}, '', '/privacy');
    state.auth = 'loading';
    render(<App />);
    expect(screen.getByText('PRIVACY_POLICY')).toBeInTheDocument();
    expect(document.querySelector('.MuiCircularProgress-root')).toBeNull();
  });

  it('renders TermsOfService at /terms without waiting for auth', () => {
    window.history.pushState({}, '', '/terms');
    state.auth = 'loading';
    render(<App />);
    expect(screen.getByText('TERMS_OF_SERVICE')).toBeInTheDocument();
    expect(document.querySelector('.MuiCircularProgress-root')).toBeNull();
  });

  it('renders PrivacyPolicy at /privacy even when authenticated', () => {
    window.history.pushState({}, '', '/privacy');
    state.auth = 'authenticated';
    render(<App />);
    expect(screen.getByText('PRIVACY_POLICY')).toBeInTheDocument();
    expect(screen.queryByText('APP_SHELL')).not.toBeInTheDocument();
  });

  it('renders TermsOfService at /terms even when authenticated', () => {
    window.history.pushState({}, '', '/terms');
    state.auth = 'authenticated';
    render(<App />);
    expect(screen.getByText('TERMS_OF_SERVICE')).toBeInTheDocument();
    expect(screen.queryByText('APP_SHELL')).not.toBeInTheDocument();
  });

  it('Snackbar onClose fires setError(null) on autohide timeout', async () => {
    vi.useFakeTimers();
    state.auth = 'anonymous';
    state.error = 'temp';
    render(<App />);
    await act(async () => {
      // MUI Snackbar autoHideDuration is 6000ms; advance past it.
      vi.advanceTimersByTime(6500);
    });
    vi.useRealTimers();
    expect(state.setError).toHaveBeenCalledWith(null);
  });
});

describe('friend-accept token bootstrap', () => {
  beforeEach(() => {
    window.sessionStorage.clear();
  });

  it('does not POST while anonymous; preserves the token in URL', async () => {
    state.auth = 'anonymous';
    window.history.pushState({}, '', '/?friend_accept=tok1');
    render(<App />);
    expect(h.api.acceptFriendToken).not.toHaveBeenCalled();
    expect(window.location.search).toBe('?friend_accept=tok1');
  });

  it('POSTs and shows a success notice when authenticated', async () => {
    h.api.acceptFriendToken.mockResolvedValueOnce({
      friendship: { friend_id: 9, status: 'accepted', direction: '', requested_at: '' },
    });
    state.auth = 'authenticated';
    state.users = [{ id: 9, name: 'Alice' }];
    window.history.pushState({}, '', '/?friend_accept=tokA');
    render(<App />);
    await new Promise((r) => setTimeout(r, 0));
    expect(h.api.acceptFriendToken).toHaveBeenCalledWith('tokA');
    expect(state.setNotice).toHaveBeenCalledWith({
      message: "You're now friends with Alice.",
      severity: 'success',
    });
    expect(window.location.search).toBe('');
    expect(state.refreshNotifications).toHaveBeenCalled();
  });

  it('shows an info notice when the request was already accepted', async () => {
    h.api.acceptFriendToken.mockResolvedValueOnce({ already: true });
    state.auth = 'authenticated';
    window.history.pushState({}, '', '/?friend_accept=tokB');
    render(<App />);
    await new Promise((r) => setTimeout(r, 0));
    expect(state.setNotice).toHaveBeenCalledWith({
      message: "You're already friends — nothing to accept.",
      severity: 'info',
    });
  });

  it('surfaces a server error via setError, not setNotice', async () => {
    const err = new Error("this invitation isn't for your account");
    h.api.acceptFriendToken.mockRejectedValueOnce(err);
    state.auth = 'authenticated';
    window.history.pushState({}, '', '/?friend_accept=tokC');
    render(<App />);
    await new Promise((r) => setTimeout(r, 0));
    expect(state.setError).toHaveBeenCalledWith("this invitation isn't for your account");
    expect(state.setNotice).not.toHaveBeenCalled();
  });

  it('falls back to sessionStorage when URL token is absent (post-OAuth)', async () => {
    h.api.acceptFriendToken.mockResolvedValueOnce({ already: true });
    state.auth = 'authenticated';
    window.history.pushState({}, '', '/');
    window.sessionStorage.setItem('aerly.pending_friend_accept', 'stashed-tok');
    render(<App />);
    await new Promise((r) => setTimeout(r, 0));
    expect(h.api.acceptFriendToken).toHaveBeenCalledWith('stashed-tok');
    expect(window.sessionStorage.getItem('aerly.pending_friend_accept')).toBeNull();
  });
});

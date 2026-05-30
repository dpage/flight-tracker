import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

const h = vi.hoisted(() => {
  return {
    connectSSE: vi.fn(
      (_handlers: {
        onPlanPart: (p: unknown) => void;
        onTrip?: (id: number) => void;
        onPlan?: (tripId: number) => void;
        onNotifications: (n: unknown) => void;
      }) => vi.fn(),
    ),
    api: {
      acceptFriendToken: vi.fn(),
    },
    state: {
      auth: 'loading' as 'loading' | 'anonymous' | 'authenticated',
      error: null as string | null,
      notice: null as { message: string; severity: 'success' | 'info' } | null,
      currentTrip: null as { id: number } | null,
      init: vi.fn(),
      setError: vi.fn(),
      setNotice: vi.fn(),
      refreshNotifications: vi.fn(),
      refreshFriendships: vi.fn(),
      refreshUsers: vi.fn(),
      applyPlanPartUpdate: vi.fn(),
      loadTrip: vi.fn(),
      loadTracker: vi.fn(),
      applyNotificationsUpdate: vi.fn(),
      users: [] as Array<{ id: number; name: string }>,
    },
  };
});
const connectSSE = h.connectSSE;
const state = h.state;

vi.mock('./sse', () => ({ connectSSE: h.connectSSE }));
vi.mock('./api/client', () => ({ api: h.api, ApiError: class {} }));
// The authenticated `/` route renders Layout → TripList. Mock the chrome to a
// plain Outlet so the routed page shows through, and stub the pages.
vi.mock('./components/Layout', async () => {
  const { Outlet } = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return {
    default: () => (
      <div>
        LAYOUT
        <Outlet />
      </div>
    ),
  };
});
vi.mock('./pages/TripList', () => ({ default: () => <div>TRIP_LIST</div> }));
vi.mock('./pages/TripDetail', () => ({ default: () => <div>TRIP_DETAIL</div> }));
vi.mock('./pages/TripTimeline', () => ({ default: () => <div>TRIP_TIMELINE</div> }));
vi.mock('./pages/TripMap', () => ({ default: () => <div>TRIP_MAP</div> }));
vi.mock('./pages/Tracker', () => ({ default: () => <div>TRACKER</div> }));
vi.mock('./components/Login', () => ({ default: () => <div>LOGIN</div> }));
vi.mock('./components/PrivacyPolicy', () => ({ default: () => <div>PRIVACY_POLICY</div> }));
vi.mock('./components/TermsOfService', () => ({ default: () => <div>TERMS_OF_SERVICE</div> }));
vi.mock('./state/store', () => {
  const useStore = (sel: (s: typeof h.state) => unknown) => sel(h.state);
  // App's defensive onTrip/onPlan SSE handlers read useStore.getState().
  useStore.getState = () => h.state;
  return { useStore };
});

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

  it('renders the trip list home and wires SSE when authenticated', () => {
    state.auth = 'authenticated';
    render(<App />);
    expect(screen.getByText('TRIP_LIST')).toBeInTheDocument();
    expect(connectSSE).toHaveBeenCalledTimes(1);
    const handlers = connectSSE.mock.calls[0][0];
    // plan_part.updated → applyPlanPartUpdate folds the live part into the
    // tracker list and the open trip's timeline.
    handlers.onPlanPart({ plan_part_id: 7 });
    expect(state.applyPlanPartUpdate).toHaveBeenCalledWith({ plan_part_id: 7 });
    handlers.onNotifications({ friend_requests_pending: 2 });
    expect(state.applyNotificationsUpdate).toHaveBeenCalledWith({ friend_requests_pending: 2 });
    // notifications.updated fires on any friendship state change for the
    // viewer — the friend list and the cached user records have to be
    // refreshed so newly-accepted friends show up in the share/passenger
    // pickers and the friends dialog (instead of "User #N").
    expect(state.refreshFriendships).toHaveBeenCalled();
    expect(state.refreshUsers).toHaveBeenCalled();
  });

  it('defensive onTrip/onPlan refetch only the open trip (backend does not emit these yet)', () => {
    state.auth = 'authenticated';
    state.currentTrip = { id: 5 };
    render(<App />);
    const handlers = connectSSE.mock.calls[0][0];
    // A matching trip id refetches; a non-matching one is ignored.
    handlers.onTrip!(5);
    expect(state.loadTrip).toHaveBeenCalledWith(5);
    state.loadTrip.mockClear();
    handlers.onTrip!(99);
    expect(state.loadTrip).not.toHaveBeenCalled();
    // onPlan refetches the open trip when it matches, and always the tracker.
    handlers.onPlan!(5);
    expect(state.loadTrip).toHaveBeenCalledWith(5);
    expect(state.loadTracker).toHaveBeenCalled();
  });

  it('renders the success-notice snackbar and clears it via the close button', async () => {
    state.auth = 'anonymous';
    state.notice = { message: 'cheer', severity: 'success' };
    render(<App />);
    expect(screen.getByText('cheer')).toBeInTheDocument();
    // Two MUI Snackbars are mounted (error + notice); pick the close button
    // that lives inside the Alert wrapping our notice text.
    const noticeAlert = screen.getByText('cheer').closest('.MuiAlert-root');
    expect(noticeAlert).not.toBeNull();
    const closeBtn = noticeAlert!.querySelector('button[aria-label="Close"]') as HTMLElement;
    expect(closeBtn).not.toBeNull();
    await userEvent.click(closeBtn);
    expect(state.setNotice).toHaveBeenCalledWith(null);
  });

  it('Snackbar onClose fires setNotice(null) on autohide timeout', async () => {
    vi.useFakeTimers();
    state.auth = 'anonymous';
    state.notice = { message: 'temp', severity: 'info' };
    render(<App />);
    await act(async () => {
      vi.advanceTimersByTime(6500);
    });
    vi.useRealTimers();
    expect(state.setNotice).toHaveBeenCalledWith(null);
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
    expect(screen.queryByText('TRIP_LIST')).not.toBeInTheDocument();
  });

  it('renders TermsOfService at /terms even when authenticated', () => {
    window.history.pushState({}, '', '/terms');
    state.auth = 'authenticated';
    render(<App />);
    expect(screen.getByText('TERMS_OF_SERVICE')).toBeInTheDocument();
    expect(screen.queryByText('TRIP_LIST')).not.toBeInTheDocument();
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

  it('uses "them" when accept-token returns a friendship but the friend is unknown', async () => {
    h.api.acceptFriendToken.mockResolvedValueOnce({
      friendship: { friend_id: 999, status: 'accepted', direction: '', requested_at: '' },
    });
    state.auth = 'authenticated';
    state.users = []; // friend_id 999 won't resolve
    window.history.pushState({}, '', '/?friend_accept=tokU');
    render(<App />);
    await new Promise((r) => setTimeout(r, 0));
    expect(state.setNotice).toHaveBeenCalledWith({
      message: "You're now friends with them.",
      severity: 'success',
    });
  });

  it('uses "them" when neither friendship nor already is returned', async () => {
    h.api.acceptFriendToken.mockResolvedValueOnce({});
    state.auth = 'authenticated';
    window.history.pushState({}, '', '/?friend_accept=tokE');
    render(<App />);
    await new Promise((r) => setTimeout(r, 0));
    expect(state.setNotice).toHaveBeenCalledWith({
      message: "You're now friends with them.",
      severity: 'success',
    });
  });

  it('falls back to "them" when the friend has no display name', async () => {
    h.api.acceptFriendToken.mockResolvedValueOnce({
      friendship: { friend_id: 11, status: 'accepted', direction: '', requested_at: '' },
    });
    state.auth = 'authenticated';
    state.users = [{ id: 11, name: '   ' }]; // whitespace name → trim() to ''
    window.history.pushState({}, '', '/?friend_accept=tokWS');
    render(<App />);
    await new Promise((r) => setTimeout(r, 0));
    expect(state.setNotice).toHaveBeenCalledWith({
      message: "You're now friends with them.",
      severity: 'success',
    });
  });

  it('stringifies non-Error rejections via setError', async () => {
    h.api.acceptFriendToken.mockRejectedValueOnce('plain-string-failure');
    state.auth = 'authenticated';
    window.history.pushState({}, '', '/?friend_accept=tokS');
    render(<App />);
    await new Promise((r) => setTimeout(r, 0));
    expect(state.setError).toHaveBeenCalledWith('plain-string-failure');
  });

  it('preserves other query params when stripping friend_accept from the URL', async () => {
    h.api.acceptFriendToken.mockResolvedValueOnce({ already: true });
    state.auth = 'authenticated';
    window.history.pushState({}, '', '/?friend_accept=tokQ&foo=bar');
    render(<App />);
    await new Promise((r) => setTimeout(r, 0));
    expect(window.location.search).toBe('?foo=bar');
  });

  it('treats sessionStorage.getItem throws as no token', async () => {
    state.auth = 'authenticated';
    window.history.pushState({}, '', '/');
    const originalStorage = window.sessionStorage;
    Object.defineProperty(window, 'sessionStorage', {
      configurable: true,
      value: {
        getItem: () => {
          throw new Error('blocked');
        },
        setItem: () => {},
        removeItem: () => {},
        clear: () => {},
        key: () => null,
        length: 0,
      },
    });
    try {
      render(<App />);
      await new Promise((r) => setTimeout(r, 0));
      expect(h.api.acceptFriendToken).not.toHaveBeenCalled();
    } finally {
      Object.defineProperty(window, 'sessionStorage', {
        configurable: true,
        value: originalStorage,
      });
    }
  });

  it('silently drops the stash cleanup when sessionStorage.removeItem throws', async () => {
    h.api.acceptFriendToken.mockResolvedValueOnce({ already: true });
    state.auth = 'authenticated';
    window.history.pushState({}, '', '/');
    const originalStorage = window.sessionStorage;
    Object.defineProperty(window, 'sessionStorage', {
      configurable: true,
      value: {
        getItem: () => 'stashed-tok',
        setItem: () => {},
        removeItem: () => {
          throw new Error('blocked');
        },
        clear: () => {},
        key: () => null,
        length: 0,
      },
    });
    try {
      render(<App />);
      await new Promise((r) => setTimeout(r, 0));
      // The accept still ran; the removeItem failure was swallowed.
      expect(h.api.acceptFriendToken).toHaveBeenCalledWith('stashed-tok');
    } finally {
      Object.defineProperty(window, 'sessionStorage', {
        configurable: true,
        value: originalStorage,
      });
    }
  });
});

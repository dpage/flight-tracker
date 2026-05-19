import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

const h = vi.hoisted(() => {
  return {
    connectSSE: vi.fn((_cb: (f: unknown) => void) => vi.fn()),
    state: {
      auth: 'loading' as 'loading' | 'anonymous' | 'authenticated',
      error: null as string | null,
      init: vi.fn(),
      setError: vi.fn(),
      applyFlightUpdate: vi.fn(),
    },
  };
});
const connectSSE = h.connectSSE;
const state = h.state;

vi.mock('./sse', () => ({ connectSSE: h.connectSSE }));
vi.mock('./components/AppShell', () => ({ default: () => <div>APP_SHELL</div> }));
vi.mock('./components/Login', () => ({ default: () => <div>LOGIN</div> }));
vi.mock('./state/store', () => ({
  useStore: (sel: (s: typeof h.state) => unknown) => sel(h.state),
}));

import App from './App';

beforeEach(() => {
  vi.clearAllMocks();
  state.auth = 'loading';
  state.error = null;
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
    // The SSE callback should forward to applyFlightUpdate.
    const cb = connectSSE.mock.calls[0][0];
    cb({ id: 7 });
    expect(state.applyFlightUpdate).toHaveBeenCalledWith({ id: 7 });
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

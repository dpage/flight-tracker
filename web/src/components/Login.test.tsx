import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';

const h = vi.hoisted(() => ({
  api: {
    getAuthProviders: vi.fn(),
    getDevAuthBypassEnabled: vi.fn(),
  },
}));

vi.mock('../api/client', () => ({ api: h.api }));

import Login from './Login';

describe('Login', () => {
  beforeEach(() => {
    h.api.getAuthProviders.mockReset();
    h.api.getDevAuthBypassEnabled.mockReset();
    h.api.getAuthProviders.mockResolvedValue([
      { name: 'github', label: 'GitHub' },
    ]);
  });

  it('renders the heading and a GitHub sign-in link', async () => {
    h.api.getDevAuthBypassEnabled.mockResolvedValue(false);
    render(<Login />);
    expect(screen.getByRole('heading', { name: 'Aerly' })).toBeInTheDocument();
    const link = await screen.findByRole('link', { name: /sign in with github/i });
    expect(link).toHaveAttribute('href', '/auth/github/login');
    await waitFor(() => expect(h.api.getDevAuthBypassEnabled).toHaveBeenCalled());
  });

  it('renders one button per configured provider', async () => {
    h.api.getAuthProviders.mockResolvedValue([
      { name: 'github', label: 'GitHub' },
      { name: 'google', label: 'Google' },
    ]);
    h.api.getDevAuthBypassEnabled.mockResolvedValue(false);
    render(<Login />);
    const gh = await screen.findByRole('link', { name: /sign in with github/i });
    const goog = await screen.findByRole('link', { name: /sign in with google/i });
    expect(gh).toHaveAttribute('href', '/auth/github/login');
    expect(goog).toHaveAttribute('href', '/auth/google/login');
  });

  it('does not render the dev-login form when DEV_AUTH_BYPASS is off', async () => {
    h.api.getDevAuthBypassEnabled.mockResolvedValue(false);
    render(<Login />);
    await waitFor(() => expect(h.api.getDevAuthBypassEnabled).toHaveBeenCalled());
    expect(
      screen.queryByRole('button', { name: /sign in as dev user/i }),
    ).not.toBeInTheDocument();
  });

  it('renders the dev-login form when DEV_AUTH_BYPASS is enabled', async () => {
    h.api.getDevAuthBypassEnabled.mockResolvedValue(true);
    render(<Login />);
    const submit = await screen.findByRole('button', {
      name: /sign in as dev user/i,
    });
    // Submit is inside a plain GET form pointed at /auth/dev-login. We
    // verify the form action/method and the input name so the browser
    // navigates to /auth/dev-login?login=<value>.
    const form = submit.closest('form');
    expect(form).not.toBeNull();
    expect(form).toHaveAttribute('action', '/auth/dev-login');
    expect(form?.getAttribute('method')?.toLowerCase()).toBe('get');
    const input = screen.getByLabelText(/dev login username/i);
    expect(input).toHaveAttribute('name', 'login');
  });
});

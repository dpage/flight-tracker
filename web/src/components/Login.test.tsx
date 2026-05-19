import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';

import Login from './Login';

describe('Login', () => {
  it('renders the heading and a GitHub sign-in link', () => {
    render(<Login />);
    expect(screen.getByRole('heading', { name: 'Flight Tracker' })).toBeInTheDocument();
    const link = screen.getByRole('link', { name: /sign in with github/i });
    expect(link).toHaveAttribute('href', '/auth/github/login');
  });
});

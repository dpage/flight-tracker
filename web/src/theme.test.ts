import { describe, it, expect } from 'vitest';

import { theme } from './theme';

describe('theme', () => {
  it('exposes the configured palette and shape', () => {
    expect(theme.palette.mode).toBe('light');
    expect(theme.palette.primary.main).toBe('#1f5fa8');
    expect(theme.palette.secondary.main).toBe('#d97706');
    expect(theme.palette.background.default).toBe('#f5f6fa');
    expect(theme.shape.borderRadius).toBe(8);
    expect(theme.typography.fontFamily).toContain('system-ui');
  });
});

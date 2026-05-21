import { describe, it, expect } from 'vitest';

import { fmtAgo, fmtDateTime, fmtRelative, fmtUTC } from './format';

describe('fmtDateTime', () => {
  it('renders an airport-local time when tz is provided', () => {
    // 10:00Z in BST (Europe/London, July) = 11:00.
    expect(fmtDateTime('2024-07-01T10:00:00Z', 'Europe/London')).toMatch(/11:00/);
  });

  it('falls back to UTC with an explicit suffix when tz is missing', () => {
    expect(fmtDateTime('2024-07-01T10:00:00Z')).toMatch(/10:00 UTC$/);
  });

  it('treats an empty tz string as missing', () => {
    expect(fmtDateTime('2024-07-01T10:00:00Z', '')).toMatch(/10:00 UTC$/);
  });
});

describe('fmtUTC', () => {
  it('always renders in UTC with the suffix', () => {
    expect(fmtUTC('2024-01-15T23:45:00Z')).toMatch(/23:45 UTC$/);
  });
});

describe('fmtRelative', () => {
  it('renders seconds for sub-minute durations', () => {
    expect(fmtRelative(0)).toBe('0s');
    expect(fmtRelative(42)).toBe('42s');
  });

  it('clamps negative inputs to 0', () => {
    expect(fmtRelative(-1)).toBe('0s');
  });

  it('renders minutes only when the second component is 0', () => {
    expect(fmtRelative(3 * 60)).toBe('3m');
  });

  it('renders minutes and seconds when the second component is non-zero', () => {
    expect(fmtRelative(3 * 60 + 7)).toBe('3m 7s');
  });

  it('renders hours only when the minute component is 0', () => {
    expect(fmtRelative(2 * 3600)).toBe('2h');
  });

  it('renders hours and minutes when the minute component is non-zero', () => {
    expect(fmtRelative(2 * 3600 + 5 * 60)).toBe('2h 5m');
  });
});

describe('fmtAgo', () => {
  it('returns "just now" for sub-5-second deltas', () => {
    const now = Date.UTC(2024, 0, 1, 0, 0, 10);
    expect(fmtAgo(new Date(now - 2 * 1000).toISOString(), now)).toBe('just now');
  });

  it('appends " ago" to longer deltas via fmtRelative', () => {
    const now = Date.UTC(2024, 0, 1, 0, 0, 10);
    expect(fmtAgo(new Date(now - 42 * 1000).toISOString(), now)).toBe('42s ago');
  });

  it('clamps future timestamps to "just now"', () => {
    const now = Date.UTC(2024, 0, 1, 0, 0, 10);
    expect(fmtAgo(new Date(now + 10_000).toISOString(), now)).toBe('just now');
  });
});

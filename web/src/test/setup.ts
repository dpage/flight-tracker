import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';

// Mutable module-level flag for matchMedia.matches — flip it for narrow/wide
// layout tests via setMatchMedia().
let matchMediaMatches = false;

export function setMatchMedia(matches: boolean): void {
  matchMediaMatches = matches;
}

window.matchMedia = vi.fn().mockImplementation((query: string) => ({
  matches: matchMediaMatches,
  media: query,
  onchange: null,
  addListener: vi.fn(),
  removeListener: vi.fn(),
  addEventListener: vi.fn(),
  removeEventListener: vi.fn(),
  dispatchEvent: vi.fn(),
}));

class ResizeObserverStub {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}
globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver;

afterEach(() => {
  cleanup();
  setMatchMedia(false);
});

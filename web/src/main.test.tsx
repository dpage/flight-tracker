import { describe, it, expect, beforeEach, vi } from 'vitest';

const render = vi.fn();
const createRoot = vi.fn((_el: Element | DocumentFragment) => ({ render }));

vi.mock('react-dom/client', () => ({ createRoot }));
vi.mock('maplibre-gl/dist/maplibre-gl.css', () => ({}));
// Keep App lightweight so importing main.tsx doesn't pull the whole tree.
vi.mock('./App', () => ({ default: () => null }));

beforeEach(() => {
  vi.clearAllMocks();
  vi.resetModules();
  document.body.innerHTML = '';
});

describe('main.tsx', () => {
  it('creates a root on #root and renders the app', async () => {
    document.body.innerHTML = '<div id="root"></div>';
    await import('./main');
    expect(createRoot).toHaveBeenCalledTimes(1);
    expect(createRoot.mock.calls[0][0]).toBe(document.getElementById('root'));
    expect(render).toHaveBeenCalledTimes(1);
  });

  it('throws when #root is missing', async () => {
    document.body.innerHTML = '';
    vi.resetModules();
    await expect(import('./main')).rejects.toThrow('missing #root element');
  });
});

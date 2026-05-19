import { vi } from 'vitest';

// Minimal maplibre-gl stand-in implementing exactly the surface FlightMap
// uses. Named type exports (Map/Marker/GeoJSONSource/StyleSpecification/
// LngLatBoundsLike) are erased by tsc, so only the runtime `default` matters.

export interface FakeSource {
  setData: ReturnType<typeof vi.fn>;
}

export class FakeMap {
  static instances: FakeMap[] = [];
  opts: unknown;
  handlers: Record<string, Array<() => void>> = {};
  onceHandlers: Record<string, Array<() => void>> = {};
  sources = new Map<string, FakeSource>();
  layers: unknown[] = [];
  controls: unknown[] = [];
  styleLoaded = true;
  addControl = vi.fn((ctrl: unknown) => {
    this.controls.push(ctrl);
  });
  fitBounds = vi.fn();
  remove = vi.fn();

  constructor(opts: unknown) {
    this.opts = opts;
    FakeMap.instances.push(this);
  }

  on(evt: string, cb: () => void): void {
    (this.handlers[evt] ??= []).push(cb);
    // Fire 'load' immediately so addSource/addLayer code runs deterministically.
    if (evt === 'load') cb();
  }

  once(evt: string, cb: () => void): void {
    (this.onceHandlers[evt] ??= []).push(cb);
    // Fire 'load' immediately so the deferred apply()/fit() path is exercised.
    if (evt === 'load') cb();
  }

  fire(evt: string): void {
    for (const cb of this.handlers[evt] ?? []) cb();
  }

  addSource(id: string, _spec: unknown): void {
    this.sources.set(id, { setData: vi.fn() });
  }

  addLayer(spec: unknown): void {
    this.layers.push(spec);
  }

  getSource(id: string): FakeSource | undefined {
    return this.sources.get(id);
  }

  isStyleLoaded(): boolean {
    return this.styleLoaded;
  }
}

export class FakeMarker {
  static instances: FakeMarker[] = [];
  opts: { element?: HTMLElement } | undefined;
  lngLat: [number, number] | null = null;
  rotation = 0;
  added = false;
  remove = vi.fn();

  constructor(opts?: { element?: HTMLElement }) {
    this.opts = opts;
    FakeMarker.instances.push(this);
  }

  setLngLat(ll: [number, number]): this {
    this.lngLat = ll;
    return this;
  }

  setRotation(r: number): this {
    this.rotation = r;
    return this;
  }

  addTo(): this {
    this.added = true;
    return this;
  }

  getElement(): HTMLElement {
    return this.opts?.element as HTMLElement;
  }
}

export class FakeNavigationControl {}

export function resetMaplibreMock(): void {
  FakeMap.instances = [];
  FakeMarker.instances = [];
}

const maplibregl = {
  Map: FakeMap,
  Marker: FakeMarker,
  NavigationControl: FakeNavigationControl,
};

export default maplibregl;

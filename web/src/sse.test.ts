import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

import { connectSSE } from './sse';
import type { Flight } from './api/types';

type Listener = (ev: unknown) => void;

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  opts: unknown;
  listeners: Record<string, Listener[]> = {};
  closed = false;

  constructor(url: string, opts?: unknown) {
    this.url = url;
    this.opts = opts;
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, cb: Listener): void {
    (this.listeners[type] ??= []).push(cb);
  }

  emit(type: string, ev?: unknown): void {
    for (const cb of this.listeners[type] ?? []) cb(ev);
  }

  close(): void {
    this.closed = true;
  }
}

const flight: Flight = {
  id: 1,
  ident: 'BA1',
  scheduled_out: '',
  scheduled_in: '',
  origin_iata: '',
  dest_iata: '',
  status: 'Scheduled',
  notes: '',
  passenger_ids: [],
};

beforeEach(() => {
  FakeEventSource.instances = [];
  globalThis.EventSource = FakeEventSource as unknown as typeof EventSource;
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe('connectSSE', () => {
  it('opens an EventSource against /api/events with credentials', () => {
    const teardown = connectSSE(() => {});
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(FakeEventSource.instances[0].url).toBe('/api/events');
    expect(FakeEventSource.instances[0].opts).toEqual({ withCredentials: true });
    teardown();
  });

  it('open handler resets retry backoff', () => {
    const teardown = connectSSE(() => {});
    const es = FakeEventSource.instances[0];
    // Trigger an error to bump retry, then open again to reset it.
    es.emit('error');
    vi.advanceTimersByTime(1000); // reconnect (delay was 1000)
    const es2 = FakeEventSource.instances[1];
    es2.emit('open'); // resets retry to 1000
    es2.emit('error');
    // After reset, delay should be 1000 again (not doubled).
    expect(FakeEventSource.instances).toHaveLength(2);
    vi.advanceTimersByTime(999);
    expect(FakeEventSource.instances).toHaveLength(2);
    vi.advanceTimersByTime(1);
    expect(FakeEventSource.instances).toHaveLength(3);
    teardown();
  });

  it('flight.updated with good payload calls onFlight', () => {
    const onFlight = vi.fn();
    const teardown = connectSSE(onFlight);
    FakeEventSource.instances[0].emit('flight.updated', { data: JSON.stringify(flight) });
    expect(onFlight).toHaveBeenCalledWith(flight);
    teardown();
  });

  it('bad JSON payload is caught and logged', () => {
    const err = vi.spyOn(console, 'error').mockImplementation(() => {});
    const onFlight = vi.fn();
    const teardown = connectSSE(onFlight);
    FakeEventSource.instances[0].emit('flight.updated', { data: '{not json' });
    expect(onFlight).not.toHaveBeenCalled();
    expect(err).toHaveBeenCalledWith('bad SSE payload', expect.anything());
    teardown();
  });

  it('error handler closes, reconnects, and doubles backoff capped at 30s', () => {
    const teardown = connectSSE(() => {});
    const es0 = FakeEventSource.instances[0];
    es0.emit('error');
    expect(es0.closed).toBe(true);
    // delay 1000 -> reconnect
    vi.advanceTimersByTime(1000);
    expect(FakeEventSource.instances).toHaveLength(2);
    // next backoff 2000
    FakeEventSource.instances[1].emit('error');
    vi.advanceTimersByTime(1999);
    expect(FakeEventSource.instances).toHaveLength(2);
    vi.advanceTimersByTime(1);
    expect(FakeEventSource.instances).toHaveLength(3);
    // Keep erroring to drive backoff to its 30s cap.
    for (let i = 2; i < 20; i++) {
      const es = FakeEventSource.instances[FakeEventSource.instances.length - 1];
      es.emit('error');
      vi.advanceTimersByTime(30_000);
    }
    // It still reconnects (capped, not stalled).
    expect(FakeEventSource.instances.length).toBeGreaterThan(5);
    teardown();
  });

  it('teardown stops reconnection (error after stop is a no-op)', () => {
    const teardown = connectSSE(() => {});
    const es = FakeEventSource.instances[0];
    teardown();
    expect(es.closed).toBe(true);
    es.emit('error'); // stopped -> early return, no setTimeout scheduled
    vi.advanceTimersByTime(60_000);
    expect(FakeEventSource.instances).toHaveLength(1);
  });

  it('open() early-returns when already stopped (scheduled reconnect fires after teardown)', () => {
    const teardown = connectSSE(() => {});
    FakeEventSource.instances[0].emit('error'); // schedules a reconnect in 1000ms
    teardown(); // sets stopped = true before the timer fires
    vi.advanceTimersByTime(5000);
    // open() ran but early-returned because stopped — no new EventSource.
    expect(FakeEventSource.instances).toHaveLength(1);
  });
});

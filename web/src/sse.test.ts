import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

import { connectSSE, type SSEHandlers } from './sse';
import type { TrackerPart } from './api/types';

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

const part: TrackerPart = {
  plan_part_id: 1,
  plan_id: 2,
  trip_id: 3,
  title: 'BA1',
  status: 'Enroute',
  effective_at: '2024-01-01T10:00:00Z',
  ident: 'BA1',
  dest_iata: 'JFK',
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

function noopHandlers(): SSEHandlers {
  return { onPlanPart: () => {}, onNotifications: () => {} };
}

describe('connectSSE', () => {
  it('opens an EventSource against /api/events with credentials', () => {
    const teardown = connectSSE(noopHandlers());
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(FakeEventSource.instances[0].url).toBe('/api/events');
    expect(FakeEventSource.instances[0].opts).toEqual({ withCredentials: true });
    teardown();
  });

  it('appends show_all=1 to the URL when requested', () => {
    const teardown = connectSSE(noopHandlers(), { showAll: true });
    expect(FakeEventSource.instances[0].url).toBe('/api/events?show_all=1');
    teardown();
  });

  it('open handler resets retry backoff', () => {
    const teardown = connectSSE(noopHandlers());
    const es = FakeEventSource.instances[0];
    es.emit('error');
    vi.advanceTimersByTime(1000);
    const es2 = FakeEventSource.instances[1];
    es2.emit('open');
    es2.emit('error');
    expect(FakeEventSource.instances).toHaveLength(2);
    vi.advanceTimersByTime(999);
    expect(FakeEventSource.instances).toHaveLength(2);
    vi.advanceTimersByTime(1);
    expect(FakeEventSource.instances).toHaveLength(3);
    teardown();
  });

  it('plan_part.updated with good payload calls onPlanPart', () => {
    const onPlanPart = vi.fn();
    const teardown = connectSSE({ onPlanPart, onNotifications: () => {} });
    FakeEventSource.instances[0].emit('plan_part.updated', { data: JSON.stringify(part) });
    expect(onPlanPart).toHaveBeenCalledWith(part);
    teardown();
  });

  it('bad JSON payload on plan_part.updated is caught and logged', () => {
    const err = vi.spyOn(console, 'error').mockImplementation(() => {});
    const onPlanPart = vi.fn();
    const teardown = connectSSE({ onPlanPart, onNotifications: () => {} });
    FakeEventSource.instances[0].emit('plan_part.updated', { data: '{not json' });
    expect(onPlanPart).not.toHaveBeenCalled();
    expect(err).toHaveBeenCalledWith('bad SSE payload', expect.anything());
    teardown();
  });

  it('trip.updated fires the optional onTrip with the trip id', () => {
    const onTrip = vi.fn();
    const teardown = connectSSE({ ...noopHandlers(), onTrip });
    FakeEventSource.instances[0].emit('trip.updated', { data: JSON.stringify({ id: 9 }) });
    expect(onTrip).toHaveBeenCalledWith(9);
    teardown();
  });

  it('plan.updated fires the optional onPlan with the trip id', () => {
    const onPlan = vi.fn();
    const teardown = connectSSE({ ...noopHandlers(), onPlan });
    FakeEventSource.instances[0].emit('plan.updated', { data: JSON.stringify({ trip_id: 7 }) });
    expect(onPlan).toHaveBeenCalledWith(7);
    teardown();
  });

  it('trip.updated / plan.updated are safe no-ops when no handler is supplied', () => {
    // The backend does not emit these yet; subscribing without onTrip/onPlan
    // must not throw if a stray event ever arrives.
    const teardown = connectSSE(noopHandlers());
    const es = FakeEventSource.instances[0];
    expect(() => es.emit('trip.updated', { data: JSON.stringify({ id: 1 }) })).not.toThrow();
    expect(() => es.emit('plan.updated', { data: JSON.stringify({ trip_id: 1 }) })).not.toThrow();
    teardown();
  });

  it('error handler closes, reconnects, and doubles backoff capped at 30s', () => {
    const teardown = connectSSE(noopHandlers());
    const es0 = FakeEventSource.instances[0];
    es0.emit('error');
    expect(es0.closed).toBe(true);
    vi.advanceTimersByTime(1000);
    expect(FakeEventSource.instances).toHaveLength(2);
    FakeEventSource.instances[1].emit('error');
    vi.advanceTimersByTime(1999);
    expect(FakeEventSource.instances).toHaveLength(2);
    vi.advanceTimersByTime(1);
    expect(FakeEventSource.instances).toHaveLength(3);
    for (let i = 2; i < 20; i++) {
      const es = FakeEventSource.instances[FakeEventSource.instances.length - 1];
      es.emit('error');
      vi.advanceTimersByTime(30_000);
    }
    expect(FakeEventSource.instances.length).toBeGreaterThan(5);
    teardown();
  });

  it('teardown stops reconnection (error after stop is a no-op)', () => {
    const teardown = connectSSE(noopHandlers());
    const es = FakeEventSource.instances[0];
    teardown();
    expect(es.closed).toBe(true);
    es.emit('error');
    vi.advanceTimersByTime(60_000);
    expect(FakeEventSource.instances).toHaveLength(1);
  });

  it('open() early-returns when already stopped (scheduled reconnect fires after teardown)', () => {
    const teardown = connectSSE(noopHandlers());
    FakeEventSource.instances[0].emit('error');
    teardown();
    vi.advanceTimersByTime(5000);
    expect(FakeEventSource.instances).toHaveLength(1);
  });
});

describe('notifications.updated events', () => {
  it('parses payload and forwards to onNotifications', () => {
    const onNotifications = vi.fn();
    const teardown = connectSSE({ onPlanPart: vi.fn(), onNotifications });
    const es = FakeEventSource.instances[0];
    es.emit('notifications.updated', { data: JSON.stringify({ friend_requests_pending: 4 }) });
    expect(onNotifications).toHaveBeenCalledWith({ friend_requests_pending: 4 });
    teardown();
  });

  it('logs and ignores a malformed notifications payload', () => {
    const err = vi.spyOn(console, 'error').mockImplementation(() => {});
    const onNotifications = vi.fn();
    const teardown = connectSSE({ onPlanPart: vi.fn(), onNotifications });
    const es = FakeEventSource.instances[0];
    es.emit('notifications.updated', { data: '{not-json}' });
    expect(onNotifications).not.toHaveBeenCalled();
    expect(err).toHaveBeenCalledWith('bad SSE payload', expect.anything());
    teardown();
  });
});

import type { Flight } from './api/types';

type Handler = (flight: Flight) => void;

// connectSSE returns a teardown function. It auto-reconnects with backoff on
// transient errors; the server pushes flight.updated events whenever the
// poller refreshes a flight.
export function connectSSE(onFlight: Handler): () => void {
  let es: EventSource | null = null;
  let stopped = false;
  let retry = 1000;

  function open() {
    if (stopped) return;
    es = new EventSource('/api/events', { withCredentials: true });
    es.addEventListener('open', () => {
      retry = 1000;
    });
    es.addEventListener('flight.updated', (ev) => {
      try {
        const f = JSON.parse((ev as MessageEvent).data) as Flight;
        onFlight(f);
      } catch (err) {
        console.error('bad SSE payload', err);
      }
    });
    es.addEventListener('error', () => {
      es?.close();
      es = null;
      if (stopped) return;
      const delay = Math.min(retry, 30_000);
      retry = Math.min(retry * 2, 30_000);
      setTimeout(open, delay);
    });
  }

  open();
  return () => {
    stopped = true;
    es?.close();
  };
}

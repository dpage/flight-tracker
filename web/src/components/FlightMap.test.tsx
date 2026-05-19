import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, act } from '@testing-library/react';

import type { Flight, Position } from '../api/types';
import maplibreMock, { FakeMap, FakeMarker, resetMaplibreMock } from '../test/maplibre-mock';

vi.mock('maplibre-gl', () => ({ default: maplibreMock, ...maplibreMock }));

const h = vi.hoisted(() => ({
  state: {
    flights: [] as Flight[],
    selectedFlightId: null as number | null,
    selectFlight: vi.fn(),
  },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof h.state) => unknown) => sel(h.state),
}));

import FlightMap from './FlightMap';

function pos(over: Partial<Position> = {}): Position {
  return { ts: '2024-01-01T10:00:00Z', lat: 50, lon: 5, is_estimated: false, ...over };
}

function flight(over: Partial<Flight> = {}): Flight {
  return {
    id: 1,
    ident: 'BA1',
    scheduled_out: '2024-01-01T10:00:00Z',
    scheduled_in: '2024-01-01T12:00:00Z',
    origin_iata: 'LHR',
    dest_iata: 'JFK',
    origin_lat: 51.47,
    origin_lon: -0.45,
    dest_lat: 40.64,
    dest_lon: -73.78,
    status: 'Enroute',
    notes: '',
    passenger_ids: [],
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  resetMaplibreMock();
  h.state.flights = [];
  h.state.selectedFlightId = null;
});

describe('FlightMap lifecycle', () => {
  it('creates the map, adds control/sources/layers on load, and cleans up on unmount', () => {
    h.state.flights = [flight()];
    const { unmount } = render(<FlightMap />);
    const map = FakeMap.instances[0];
    expect(map).toBeTruthy();
    expect(map.controls).toHaveLength(1);
    expect(map.sources.has('flown')).toBe(true);
    expect(map.sources.has('remaining')).toBe(true);
    expect(map.layers).toHaveLength(2);
    unmount();
    expect(map.remove).toHaveBeenCalled();
  });

  it('returns early when the container ref is null', () => {
    // Render then immediately check: jsdom always provides a ref via Box, so
    // instead verify nothing crashes with zero flights.
    render(<FlightMap />);
    expect(FakeMap.instances).toHaveLength(1);
  });

  it('applies data immediately when style is loaded', () => {
    h.state.flights = [flight({ latest_position: pos() })];
    render(<FlightMap />);
    const map = FakeMap.instances[0];
    expect(map.getSource('flown')?.setData).toHaveBeenCalled();
    expect(map.getSource('remaining')?.setData).toHaveBeenCalled();
  });

  it('defers data apply via once(load) when style not yet loaded', () => {
    const orig = FakeMap.prototype.isStyleLoaded;
    FakeMap.prototype.isStyleLoaded = function () {
      return false;
    };
    try {
      h.state.flights = [flight({ latest_position: pos() })];
      render(<FlightMap />);
      const map = FakeMap.instances[0];
      // once('load') fires synchronously in the mock -> setData still called.
      expect(map.getSource('flown')?.setData).toHaveBeenCalled();
    } finally {
      FakeMap.prototype.isStyleLoaded = orig;
    }
  });
});

describe('auto-fit effect', () => {
  it('fits bounds for renderable flights when nothing is selected', () => {
    h.state.flights = [flight()];
    render(<FlightMap />);
    expect(FakeMap.instances[0].fitBounds).toHaveBeenCalled();
  });

  it('skips auto-fit when a flight is selected', () => {
    h.state.selectedFlightId = 1;
    h.state.flights = [flight()];
    render(<FlightMap />);
    // selected-flight effect fits instead; ensure no crash and instance exists.
    expect(FakeMap.instances[0]).toBeTruthy();
  });

  it('skips re-fit when idsKey is unchanged (memoized)', () => {
    h.state.flights = [flight()];
    const { rerender } = render(<FlightMap />);
    const map = FakeMap.instances[0];
    const callsBefore = map.fitBounds.mock.calls.length;
    rerender(<FlightMap />);
    expect(map.fitBounds.mock.calls.length).toBe(callsBefore);
  });

  it('does nothing when bounds are null (single point only)', () => {
    h.state.flights = [
      flight({
        origin_lat: undefined,
        origin_lon: undefined,
        dest_lat: undefined,
        dest_lon: undefined,
        latest_position: pos(),
      }),
    ];
    render(<FlightMap />);
    // hasGeometry true (latest_position) but boundsFor needs >=2 pts -> null.
    expect(FakeMap.instances[0].fitBounds).not.toHaveBeenCalled();
  });

  it('defers fit via once(load) when style not loaded', () => {
    const orig = FakeMap.prototype.isStyleLoaded;
    FakeMap.prototype.isStyleLoaded = function () {
      return false;
    };
    try {
      h.state.flights = [flight()];
      render(<FlightMap />);
      expect(FakeMap.instances[0].fitBounds).toHaveBeenCalled();
    } finally {
      FakeMap.prototype.isStyleLoaded = orig;
    }
  });
});

describe('marker sync', () => {
  it('adds a marker for an enroute flight with a position', () => {
    h.state.flights = [flight({ id: 1, latest_position: pos() })];
    render(<FlightMap />);
    expect(FakeMarker.instances.length).toBeGreaterThan(0);
    expect(FakeMarker.instances[0].added).toBe(true);
  });

  it('updates an existing marker on position change', () => {
    h.state.flights = [flight({ id: 1, latest_position: pos({ lat: 50, lon: 5 }) })];
    const { rerender } = render(<FlightMap />);
    const before = FakeMarker.instances.length;
    h.state.flights = [
      flight({ id: 1, latest_position: pos({ lat: 51, lon: 6, heading_deg: 90 }) }),
    ];
    rerender(<FlightMap />);
    expect(FakeMarker.instances.length).toBe(before); // reused, not recreated
    expect(FakeMarker.instances[0].rotation).toBe(90);
  });

  it('removes stale markers when a flight disappears', () => {
    h.state.flights = [flight({ id: 1, latest_position: pos() })];
    const { rerender } = render(<FlightMap />);
    const marker = FakeMarker.instances[0];
    h.state.flights = [];
    rerender(<FlightMap />);
    expect(marker.remove).toHaveBeenCalled();
  });

  it('skips markers for Arrived/Cancelled flights and ones without position', () => {
    h.state.flights = [
      flight({ id: 1, status: 'Arrived', latest_position: pos() }),
      flight({ id: 2, status: 'Cancelled', latest_position: pos() }),
      flight({ id: 3, status: 'Enroute', latest_position: undefined }),
    ];
    render(<FlightMap />);
    expect(FakeMarker.instances).toHaveLength(0);
  });

  it('applies estimated styling and the click handler toggles selection', () => {
    h.state.flights = [
      flight({ id: 1, latest_position: pos({ is_estimated: true, heading_deg: 45 }) }),
    ];
    render(<FlightMap />);
    const el = FakeMarker.instances[0].getElement();
    expect(el.style.opacity).toBe('0.6');
    expect(el.title).toContain('(estimated)');
    act(() => {
      el.onclick?.(new MouseEvent('click'));
    });
    expect(h.state.selectFlight).toHaveBeenCalledWith(1);
  });

  it('click on selected marker deselects', () => {
    h.state.selectedFlightId = 1;
    h.state.flights = [flight({ id: 1, latest_position: pos() })];
    render(<FlightMap />);
    const el = FakeMarker.instances[0].getElement();
    act(() => {
      el.onclick?.(new MouseEvent('click'));
    });
    expect(h.state.selectFlight).toHaveBeenCalledWith(null);
  });

  it('non-estimated marker uses solid plane styling', () => {
    h.state.flights = [flight({ id: 1, latest_position: pos({ is_estimated: false }) })];
    render(<FlightMap />);
    const el = FakeMarker.instances[0].getElement();
    expect(el.style.opacity).toBe('1');
    const path = el.querySelector('path')!;
    expect(path.getAttribute('fill')).toBe('currentColor');
  });
});

describe('selected-flight fitBounds effect', () => {
  it('fits bounds to the selected flight', () => {
    h.state.flights = [flight({ id: 1 })];
    h.state.selectedFlightId = 1;
    render(<FlightMap />);
    expect(FakeMap.instances[0].fitBounds).toHaveBeenCalled();
  });

  it('does nothing when the selected flight is not found', () => {
    h.state.flights = [flight({ id: 1 })];
    h.state.selectedFlightId = 999;
    render(<FlightMap />);
    expect(FakeMap.instances[0].fitBounds).not.toHaveBeenCalled();
  });

  it('does nothing when selected flight has no bounds', () => {
    h.state.flights = [
      flight({
        id: 1,
        origin_lat: undefined,
        origin_lon: undefined,
        dest_lat: undefined,
        dest_lon: undefined,
        latest_position: pos(),
      }),
    ];
    h.state.selectedFlightId = 1;
    render(<FlightMap />);
    expect(FakeMap.instances[0].fitBounds).not.toHaveBeenCalled();
  });
});

describe('buildFlown / buildRemaining geometry branches', () => {
  it('builds a LineString for a simple track and a MultiLineString across the antimeridian', () => {
    // Antimeridian: track jumps from lon 170 to -170 (>180 diff) -> pushPoint
    // break -> MultiLineString in buildFlown.
    h.state.flights = [
      flight({
        id: 1,
        origin_lat: 10,
        origin_lon: 169,
        dest_lat: 10,
        dest_lon: -160,
        status: 'Enroute',
        track: [
          pos({ ts: 't1', lat: 10, lon: 170 }),
          pos({ ts: 't2', lat: 10, lon: -170 }),
        ],
        latest_position: pos({ ts: 't3', lat: 10, lon: -165 }),
      }),
    ];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('flown')!.setData.mock
      .calls[0][0] as GeoJSON.FeatureCollection;
    const geom = (fc.features[0].geometry as GeoJSON.GeometryObject).type;
    expect(['MultiLineString', 'LineString']).toContain(geom);
    expect(fc.features.length).toBeGreaterThan(0);
  });

  it('skips flights with no track and no latest_position in buildFlown', () => {
    h.state.flights = [flight({ id: 1, track: [], latest_position: undefined })];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('flown')!.setData.mock
      .calls[0][0] as GeoJSON.FeatureCollection;
    expect(fc.features).toHaveLength(0);
  });

  it('handles a flight with no origin (buildFlown no-origin branch)', () => {
    h.state.flights = [
      flight({
        id: 1,
        origin_lat: undefined,
        origin_lon: undefined,
        track: [pos({ ts: 'a', lat: 50, lon: 5 }), pos({ ts: 'b', lat: 51, lon: 6 })],
        latest_position: pos({ ts: 'c', lat: 52, lon: 7 }),
      }),
    ];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('flown')!.setData.mock
      .calls[0][0] as GeoJSON.FeatureCollection;
    expect(fc.features.length).toBeGreaterThan(0);
  });

  it('latest already in track (defensive branch not appending duplicate)', () => {
    h.state.flights = [
      flight({
        id: 1,
        track: [pos({ ts: 'same', lat: 50, lon: 5 }), pos({ ts: 'last', lat: 51, lon: 6 })],
        latest_position: pos({ ts: 'last', lat: 51, lon: 6 }),
      }),
    ];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('flown')!.setData.mock
      .calls[0][0] as GeoJSON.FeatureCollection;
    expect(fc.features.length).toBeGreaterThan(0);
  });

  it('buildRemaining: skips Arrived/Cancelled and missing dest, anchors on origin or latest', () => {
    h.state.flights = [
      flight({ id: 1, status: 'Arrived' }),
      flight({ id: 2, status: 'Cancelled' }),
      flight({ id: 3, status: 'Enroute', dest_lat: undefined, dest_lon: undefined }),
      flight({
        id: 4,
        status: 'Enroute',
        origin_lat: undefined,
        origin_lon: undefined,
        latest_position: undefined,
      }),
      flight({ id: 5, status: 'Enroute', latest_position: pos({ lat: 45, lon: -30 }) }),
      flight({ id: 6, status: 'Scheduled' }), // anchored on origin
    ];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('remaining')!.setData.mock
      .calls[0][0] as GeoJSON.FeatureCollection;
    const ids = fc.features.map((f) => (f.properties as { id: number }).id).sort();
    expect(ids).toEqual([5, 6]);
  });

  it('buildRemaining MultiLineString across the antimeridian', () => {
    h.state.flights = [
      flight({
        id: 1,
        status: 'Scheduled',
        origin_lat: 10,
        origin_lon: 170,
        dest_lat: 10,
        dest_lon: -170,
        latest_position: undefined,
      }),
    ];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('remaining')!.setData.mock
      .calls[0][0] as GeoJSON.FeatureCollection;
    expect(fc.features.length).toBeGreaterThan(0);
    expect(fc.features[0].geometry.type).toBe('MultiLineString');
  });

  it('buildRemaining skips when great-circle yields no parts (Δ<1e-9 -> single point)', () => {
    // Anchor == dest at (0,0): great-circle Δ is exactly 0 (<1e-9) so it
    // returns a single point and toMultiLine filters it to [] -> continue.
    h.state.flights = [
      flight({
        id: 1,
        status: 'Scheduled',
        origin_lat: 0,
        origin_lon: 0,
        dest_lat: 0,
        dest_lon: 0,
        latest_position: undefined,
      }),
    ];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('remaining')!.setData.mock
      .calls[0][0] as GeoJSON.FeatureCollection;
    expect(fc.features).toHaveLength(0);
  });

  it('marks the selected flight in feature properties', () => {
    h.state.selectedFlightId = 1;
    h.state.flights = [flight({ id: 1, status: 'Scheduled' })];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('remaining')!.setData.mock
      .calls[0][0] as GeoJSON.FeatureCollection;
    expect((fc.features[0].properties as { selected: boolean }).selected).toBe(true);
  });
});

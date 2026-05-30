import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render } from '@testing-library/react';

import type { Position, TrackerPart } from '../api/types';
import maplibreMock, { FakeMap, FakeMarker, resetMaplibreMock } from '../test/maplibre-mock';

vi.mock('maplibre-gl', () => ({ default: maplibreMock, ...maplibreMock }));

import TrackerMap from './TrackerMap';

function pos(over: Partial<Position> = {}): Position {
  return { ts: '2026-01-01T10:00:00Z', lat: 50, lon: 5, is_estimated: false, ...over };
}

function part(over: Partial<TrackerPart> = {}): TrackerPart {
  return {
    plan_part_id: 1,
    plan_id: 1,
    trip_id: 1,
    title: 'BA1',
    status: 'Enroute',
    effective_at: '2026-01-01T10:00:00Z',
    ident: 'BA1',
    dest_iata: 'JFK',
    latest_position: pos(),
    ...over,
  };
}

beforeEach(() => {
  resetMaplibreMock();
});

describe('TrackerMap lifecycle', () => {
  it('creates the map + control and cleans up on unmount', () => {
    const { unmount } = render(<TrackerMap parts={[part()]} />);
    const map = FakeMap.instances[0];
    expect(map).toBeTruthy();
    expect(map.controls).toHaveLength(1);
    unmount();
    expect(map.remove).toHaveBeenCalled();
  });
});

describe('marker sync', () => {
  it('adds one labelled marker per part with a position', () => {
    render(<TrackerMap parts={[part({ plan_part_id: 1 }), part({ plan_part_id: 2, ident: 'LH7' })]} />);
    expect(FakeMarker.instances).toHaveLength(2);
    const label = FakeMarker.instances[0].getElement().querySelector('.tm-label');
    expect(label?.textContent).toBe('BA1');
  });

  it('skips parts without a latest_position', () => {
    render(<TrackerMap parts={[part({ latest_position: undefined })]} />);
    expect(FakeMarker.instances).toHaveLength(0);
  });

  it('reuses the marker on a position update rather than recreating it', () => {
    const { rerender } = render(<TrackerMap parts={[part({ plan_part_id: 1, latest_position: pos({ lat: 50, lon: 5 }) })]} />);
    const before = FakeMarker.instances.length;
    rerender(<TrackerMap parts={[part({ plan_part_id: 1, latest_position: pos({ lat: 51, lon: 6 }) })]} />);
    expect(FakeMarker.instances).toHaveLength(before);
    expect(FakeMarker.instances[0].lngLat).toEqual([6, 51]);
  });

  it('removes a marker when its part disappears', () => {
    const { rerender } = render(<TrackerMap parts={[part({ plan_part_id: 1 })]} />);
    const marker = FakeMarker.instances[0];
    rerender(<TrackerMap parts={[]} />);
    expect(marker.remove).toHaveBeenCalled();
  });

  it('falls back to title then id for the label', () => {
    render(<TrackerMap parts={[part({ ident: '', title: 'Hotel run', plan_part_id: 9 })]} />);
    const label = FakeMarker.instances[0].getElement().querySelector('.tm-label');
    expect(label?.textContent).toBe('Hotel run');
  });
});

describe('fit / focus', () => {
  it('fits bounds across the in-window cluster', () => {
    render(
      <TrackerMap
        parts={[
          part({ plan_part_id: 1, latest_position: pos({ lat: 50, lon: 5 }) }),
          part({ plan_part_id: 2, latest_position: pos({ lat: 40, lon: -70 }) }),
        ]}
      />,
    );
    expect(FakeMap.instances[0].fitBounds).toHaveBeenCalled();
  });

  it('focuses a single part: only that marker, flyTo not fitBounds', () => {
    render(
      <TrackerMap
        focusedPartId={2}
        parts={[
          part({ plan_part_id: 1, latest_position: pos({ lat: 50, lon: 5 }) }),
          part({ plan_part_id: 2, ident: 'LH7', latest_position: pos({ lat: 40, lon: -70 }) }),
        ]}
      />,
    );
    expect(FakeMarker.instances).toHaveLength(1);
    expect(FakeMap.instances[0].flyTo).toHaveBeenCalled();
    expect(FakeMap.instances[0].fitBounds).not.toHaveBeenCalled();
  });
});

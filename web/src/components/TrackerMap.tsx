import { useEffect, useRef } from 'react';
import maplibregl, {
  type LngLatBoundsLike,
  type Map as MlMap,
  type StyleSpecification,
} from 'maplibre-gl';
import { Box } from '@mui/material';

import type { TrackerPart } from '../api/types';

const STYLE: StyleSpecification = {
  version: 8,
  glyphs: 'https://demotiles.maplibre.org/font/{fontstack}/{range}.pbf',
  sources: {
    osm: {
      type: 'raster',
      tiles: ['https://tile.openstreetmap.org/{z}/{x}/{y}.png'],
      tileSize: 256,
      maxzoom: 19,
      attribution: '&copy; OpenStreetMap contributors',
    },
  },
  layers: [{ id: 'osm', type: 'raster', source: 'osm' }],
};

interface TrackerMapProps {
  /** Parts to plot — only those with a `latest_position` get a marker. */
  parts: TrackerPart[];
  /** When set, the map focuses (fits to) the single part with this id. */
  focusedPartId?: number | null;
}

/** Convergence map for the tracker (PRD §6.5). Plots the latest known position
 * of every in-window trackable part as a labelled marker — "who's on their way"
 * as a single live map, no ranking. When `focusedPartId` is set the view fits
 * to that one part (the single-flight focus opened from a timeline card).
 *
 * Mirrors FlightMap's MapLibre lifecycle (init once, sync markers on data
 * change, fit bounds) but works off the lighter `TrackerPart` shape, which only
 * carries a latest position. */
export default function TrackerMap({ parts, focusedPartId }: TrackerMapProps) {
  const containerRef = useRef<HTMLElement | null>(null);
  const mapRef = useRef<MlMap | null>(null);
  const markersRef = useRef<Map<number, maplibregl.Marker>>(new Map());

  useEffect(() => {
    if (!containerRef.current) return;
    const map = new maplibregl.Map({
      container: containerRef.current,
      style: STYLE,
      center: [5, 50],
      zoom: 3,
    });
    map.addControl(new maplibregl.NavigationControl(), 'top-right');
    mapRef.current = map;
    return () => {
      markersRef.current.forEach((m) => m.remove());
      markersRef.current.clear();
      map.remove();
      mapRef.current = null;
    };
  }, []);

  // Sync one marker per plotted part. Markers carry a text label (the ident, or
  // the title as a fallback) so a cluster of friends heading to the same place
  // reads at a glance.
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const live = new Set<number>();
    const plotted = focusedPartId != null ? parts.filter((p) => p.plan_part_id === focusedPartId) : parts;

    for (const p of plotted) {
      const pos = p.latest_position;
      if (!pos) continue;
      live.add(p.plan_part_id);
      const focused = focusedPartId === p.plan_part_id;
      let marker = markersRef.current.get(p.plan_part_id);
      const label = p.ident || p.title || `#${p.plan_part_id}`;
      const el = marker?.getElement() ?? buildMarkerEl();
      styleMarker(el, label, focused, pos.is_estimated);
      if (!marker) {
        marker = new maplibregl.Marker({ element: el }).setLngLat([pos.lon, pos.lat]).addTo(map);
        markersRef.current.set(p.plan_part_id, marker);
      } else {
        marker.setLngLat([pos.lon, pos.lat]);
      }
    }
    for (const [id, marker] of markersRef.current) {
      if (!live.has(id)) {
        marker.remove();
        markersRef.current.delete(id);
      }
    }
  }, [parts, focusedPartId]);

  // Fit the map: to the single focused part, or to the whole in-window cluster.
  const fittedRef = useRef<string>('');
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const plotted = (
      focusedPartId != null ? parts.filter((p) => p.plan_part_id === focusedPartId) : parts
    ).filter((p) => p.latest_position);
    const key =
      (focusedPartId ?? 'all') +
      ':' +
      plotted
        .map((p) => p.plan_part_id)
        .sort((a, b) => a - b)
        .join(',');
    if (key === fittedRef.current) return;
    fittedRef.current = key;
    const pts: [number, number][] = plotted.map((p) => [
      p.latest_position!.lon,
      p.latest_position!.lat,
    ]);
    if (focusedPartId != null && pts.length === 1) {
      const fly = () => map.flyTo({ center: pts[0], zoom: 5, duration: 600 });
      if (map.isStyleLoaded()) fly();
      else map.once('load', fly);
      return;
    }
    const bounds = boundsFor(pts);
    if (!bounds) return;
    const fit = () => map.fitBounds(bounds, { padding: 80, maxZoom: 6, duration: 600 });
    if (map.isStyleLoaded()) fit();
    else map.once('load', fit);
  }, [parts, focusedPartId]);

  return <Box ref={containerRef} sx={{ position: 'absolute', inset: 0 }} data-testid="tracker-map" />;
}

function buildMarkerEl(): HTMLElement {
  const el = document.createElement('div');
  el.style.display = 'flex';
  el.style.alignItems = 'center';
  el.style.gap = '4px';
  el.style.cursor = 'default';
  el.innerHTML = `
    <svg class="tm-icon" viewBox="0 0 24 24" width="22" height="22" fill="currentColor"
         style="filter: drop-shadow(0 1px 2px rgba(0,0,0,0.4))">
      <path d="M12 2 L13.2 11 L22 15 L22 17 L13.2 14.5 L13 20 L16 22 L16 23 L12 22 L8 23 L8 22 L11 20 L10.8 14.5 L2 17 L2 15 L10.8 11 Z"/>
    </svg>
    <span class="tm-label" style="font:600 11px/1 system-ui,-apple-system,sans-serif;
      background:rgba(255,255,255,0.9);color:#111;padding:2px 5px;border-radius:4px;
      white-space:nowrap;box-shadow:0 1px 2px rgba(0,0,0,0.3)"></span>`;
  return el;
}

function styleMarker(el: HTMLElement, label: string, focused: boolean, estimated: boolean): void {
  el.style.color = focused ? '#d97706' : '#1f5fa8';
  el.style.opacity = estimated ? '0.7' : '1';
  el.title = label + (estimated ? ' (estimated)' : '');
  const span = el.querySelector('.tm-label');
  if (span) span.textContent = label;
}

function boundsFor(pts: [number, number][]): LngLatBoundsLike | null {
  if (pts.length < 2) return null;
  let west = pts[0][0],
    east = pts[0][0],
    south = pts[0][1],
    north = pts[0][1];
  for (const [lon, lat] of pts) {
    west = Math.min(west, lon);
    east = Math.max(east, lon);
    south = Math.min(south, lat);
    north = Math.max(north, lat);
  }
  return [
    [west, south],
    [east, north],
  ];
}

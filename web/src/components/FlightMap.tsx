import { useEffect, useMemo, useRef } from 'react';
import maplibregl, {
  type LngLatBoundsLike,
  type Map as MlMap,
  type StyleSpecification,
} from 'maplibre-gl';
import { Box } from '@mui/material';

import { useStore } from '../state/store';
import { greatCircle, toMultiLine } from '../lib/great-circle';
import type { Flight } from '../api/types';

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

export default function FlightMap() {
  const flights = useStore((s) => s.flights);
  const selectedFlightId = useStore((s) => s.selectedFlightId);
  const selectFlight = useStore((s) => s.selectFlight);

  const containerRef = useRef<HTMLElement | null>(null);
  const mapRef = useRef<MlMap | null>(null);
  const markersRef = useRef<Map<number, maplibregl.Marker>>(new Map());

  const arcsGeoJSON = useMemo(() => buildArcs(flights, selectedFlightId), [flights, selectedFlightId]);

  // Initialise the MapLibre instance once.
  useEffect(() => {
    if (!containerRef.current) return;
    const map = new maplibregl.Map({
      container: containerRef.current,
      style: STYLE,
      center: [5, 50],
      zoom: 3,
    });
    map.addControl(new maplibregl.NavigationControl(), 'top-right');
    map.on('load', () => {
      map.addSource('arcs', { type: 'geojson', data: emptyFC() });
      map.addLayer({
        id: 'arcs-line',
        type: 'line',
        source: 'arcs',
        paint: {
          'line-color': ['case', ['get', 'selected'], '#d97706', '#1f5fa8'],
          'line-width': ['case', ['get', 'selected'], 3, 2],
          'line-dasharray': [2, 1.5],
          'line-opacity': 0.85,
        },
      });
    });
    mapRef.current = map;
    return () => {
      markersRef.current.forEach((m) => m.remove());
      markersRef.current.clear();
      map.remove();
      mapRef.current = null;
    };
  }, []);

  // Push arc updates whenever flights change.
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const apply = () => {
      const src = map.getSource('arcs') as maplibregl.GeoJSONSource | undefined;
      if (src) src.setData(arcsGeoJSON);
    };
    if (map.isStyleLoaded()) apply();
    else map.once('load', apply);
  }, [arcsGeoJSON]);

  // Auto-fit the map when the set of renderable flights changes — keeps newly
  // added flights from being off-screen. Skipped if the user has a flight
  // selected (the next effect handles that case).
  const fittedIdsRef = useRef<string>('');
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    if (selectedFlightId != null) return;
    const renderable = flights.filter((f) => hasGeometry(f));
    const idsKey = renderable
      .map((f) => f.id)
      .sort((a, b) => a - b)
      .join(',');
    if (idsKey === fittedIdsRef.current) return;
    fittedIdsRef.current = idsKey;
    const bounds = allFlightsBounds(renderable);
    if (!bounds) return;
    const fit = () => map.fitBounds(bounds, { padding: 80, maxZoom: 6, duration: 600 });
    if (map.isStyleLoaded()) fit();
    else map.once('load', fit);
  }, [flights, selectedFlightId]);

  // Sync plane markers with the latest_position on each flight.
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const live = new Set<number>();
    for (const f of flights) {
      const pos = f.latest_position;
      if (!pos || f.status === 'Arrived' || f.status === 'Cancelled') continue;
      live.add(f.id);
      let marker = markersRef.current.get(f.id);
      const el = marker?.getElement() ?? buildMarkerEl();
      const heading = pos.heading_deg ?? 0;
      stylePlane(el, f.id === selectedFlightId, f.ident);
      el.onclick = (e) => {
        e.stopPropagation();
        selectFlight(f.id === selectedFlightId ? null : f.id);
      };
      if (!marker) {
        // rotationAlignment: 'map' keeps the plane's heading consistent with
        // the compass even when the user rotates the map.
        marker = new maplibregl.Marker({
          element: el,
          rotation: heading,
          rotationAlignment: 'map',
        })
          .setLngLat([pos.lon, pos.lat])
          .addTo(map);
        markersRef.current.set(f.id, marker);
      } else {
        marker.setLngLat([pos.lon, pos.lat]);
        marker.setRotation(heading);
      }
    }
    for (const [id, marker] of markersRef.current) {
      if (!live.has(id)) {
        marker.remove();
        markersRef.current.delete(id);
      }
    }
  }, [flights, selectedFlightId, selectFlight]);

  // When the user picks a flight, zoom to its bounding box.
  useEffect(() => {
    const map = mapRef.current;
    if (!map || selectedFlightId == null) return;
    const f = flights.find((x) => x.id === selectedFlightId);
    if (!f) return;
    const bounds = flightBounds(f);
    if (bounds) map.fitBounds(bounds, { padding: 80, maxZoom: 7, duration: 600 });
  }, [selectedFlightId, flights]);

  return <Box ref={containerRef} sx={{ position: 'absolute', inset: 0 }} />;
}

function buildMarkerEl(): HTMLElement {
  // The SVG below points NORTH (nose at the top, tail at the bottom) when
  // unrotated, so MapLibre's Marker.setRotation(headingDeg) with
  // rotationAlignment: 'map' renders the plane pointing in its compass
  // direction of travel. Do NOT set transform on this element — MapLibre owns
  // the wrapper's transform and composites our rotation in for us.
  const el = document.createElement('div');
  el.style.width = '32px';
  el.style.height = '32px';
  el.style.display = 'grid';
  el.style.placeItems = 'center';
  el.style.cursor = 'pointer';
  el.innerHTML = `
    <svg viewBox="0 0 24 24" width="28" height="28" fill="currentColor"
         style="filter: drop-shadow(0 1px 2px rgba(0,0,0,0.4))">
      <path d="M12 2 L13.2 11 L22 15 L22 17 L13.2 14.5 L13 20 L16 22 L16 23 L12 22 L8 23 L8 22 L11 20 L10.8 14.5 L2 17 L2 15 L10.8 11 Z"/>
    </svg>`;
  return el;
}

function stylePlane(el: HTMLElement, selected: boolean, title: string) {
  el.style.color = selected ? '#d97706' : '#1f5fa8';
  el.title = title;
}

function emptyFC(): GeoJSON.FeatureCollection {
  return { type: 'FeatureCollection', features: [] };
}

function buildArcs(flights: Flight[], selectedId: number | null): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = [];
  for (const f of flights) {
    if (
      f.origin_lat == null ||
      f.origin_lon == null ||
      f.dest_lat == null ||
      f.dest_lon == null
    ) {
      continue;
    }
    const coords = greatCircle(f.origin_lat, f.origin_lon, f.dest_lat, f.dest_lon);
    const parts = toMultiLine(coords);
    features.push({
      type: 'Feature',
      properties: { id: f.id, selected: f.id === selectedId },
      geometry:
        parts.length === 1
          ? { type: 'LineString', coordinates: parts[0] }
          : { type: 'MultiLineString', coordinates: parts },
    });
  }
  return { type: 'FeatureCollection', features };
}

function flightBounds(f: Flight): LngLatBoundsLike | null {
  return boundsFor(flightPoints(f));
}

function allFlightsBounds(flights: Flight[]): LngLatBoundsLike | null {
  const pts: [number, number][] = [];
  for (const f of flights) pts.push(...flightPoints(f));
  return boundsFor(pts);
}

function flightPoints(f: Flight): [number, number][] {
  const pts: [number, number][] = [];
  if (f.origin_lon != null && f.origin_lat != null) pts.push([f.origin_lon, f.origin_lat]);
  if (f.dest_lon != null && f.dest_lat != null) pts.push([f.dest_lon, f.dest_lat]);
  if (f.latest_position) pts.push([f.latest_position.lon, f.latest_position.lat]);
  return pts;
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

function hasGeometry(f: Flight): boolean {
  return (
    (f.origin_lat != null && f.origin_lon != null && f.dest_lat != null && f.dest_lon != null) ||
    f.latest_position != null
  );
}

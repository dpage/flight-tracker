import { useEffect, useMemo, useRef } from 'react';
import maplibregl, {
  type LngLatBoundsLike,
  type Map as MlMap,
  type StyleSpecification,
} from 'maplibre-gl';
import { Box, Typography } from '@mui/material';

import { useStore } from '../state/store';
import type { Plan, PlanPart } from '../api/types';
import { greatCircle, toMultiLine } from '../lib/great-circle';

// Standard OSM raster style for the trip map (spec §11: MapLibre).
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

/** A geocoded endpoint extracted from a part, for plotting on the map. */
interface MapPoint {
  lat: number;
  lon: number;
  label: string;
}

/** Secondary trip detail tab (spec §11): the trip's geocoded parts on a
 * MapLibre map. Each part with start/end coordinates contributes a point (and,
 * when both ends are known, a great-circle leg). */
export default function TripMap() {
  const currentTrip = useStore((s) => s.currentTrip);
  const plans = useMemo(() => currentTrip?.plans ?? [], [currentTrip]);

  const points = useMemo(() => collectPoints(plans), [plans]);
  const legsFC = useMemo(() => buildLegs(plans), [plans]);

  const containerRef = useRef<HTMLElement | null>(null);
  const mapRef = useRef<MlMap | null>(null);
  const markersRef = useRef<maplibregl.Marker[]>([]);

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
      map.addSource('legs', { type: 'geojson', data: emptyFC() });
      map.addLayer({
        id: 'legs-line',
        type: 'line',
        source: 'legs',
        paint: { 'line-color': '#1f5fa8', 'line-width': 2, 'line-opacity': 0.7 },
      });
    });
    mapRef.current = map;
    return () => {
      markersRef.current.forEach((m) => m.remove());
      markersRef.current = [];
      map.remove();
      mapRef.current = null;
    };
  }, []);

  // Sync leg lines.
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const apply = () => {
      (map.getSource('legs') as maplibregl.GeoJSONSource | undefined)?.setData(legsFC);
    };
    if (map.isStyleLoaded()) apply();
    else map.once('load', apply);
  }, [legsFC]);

  // Sync point markers and fit the map to all points.
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    markersRef.current.forEach((m) => m.remove());
    markersRef.current = [];
    for (const p of points) {
      const el = document.createElement('div');
      el.style.width = '14px';
      el.style.height = '14px';
      el.style.borderRadius = '50%';
      el.style.background = '#d97706';
      el.style.border = '2px solid #fff';
      el.style.boxShadow = '0 1px 2px rgba(0,0,0,0.4)';
      el.title = p.label;
      const marker = new maplibregl.Marker({ element: el }).setLngLat([p.lon, p.lat]).addTo(map);
      markersRef.current.push(marker);
    }
    const bounds = boundsFor(points.map((p) => [p.lon, p.lat] as [number, number]));
    if (bounds) {
      const fit = () => map.fitBounds(bounds, { padding: 80, maxZoom: 9, duration: 600 });
      if (map.isStyleLoaded()) fit();
      else map.once('load', fit);
    }
  }, [points]);

  if (currentTrip && points.length === 0) {
    return (
      <Box sx={{ p: 3 }}>
        <Typography color="text.secondary">
          No mappable plans yet. Plans with a location appear here once added.
        </Typography>
      </Box>
    );
  }

  return <Box ref={containerRef} sx={{ position: 'absolute', inset: 0 }} />;
}

function collectPoints(plans: Plan[]): MapPoint[] {
  const points: MapPoint[] = [];
  for (const plan of plans) {
    for (const part of plan.parts) {
      if (part.dismissed_at) continue;
      if (part.start_lat != null && part.start_lon != null) {
        points.push({ lat: part.start_lat, lon: part.start_lon, label: part.start_label || plan.title });
      }
      if (part.end_lat != null && part.end_lon != null) {
        points.push({ lat: part.end_lat, lon: part.end_lon, label: part.end_label || plan.title });
      }
    }
  }
  return points;
}

function buildLegs(plans: Plan[]): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = [];
  for (const plan of plans) {
    for (const part of plan.parts) {
      if (part.dismissed_at) continue;
      if (!hasBothEnds(part)) continue;
      const gc = greatCircle(part.start_lat!, part.start_lon!, part.end_lat!, part.end_lon!);
      const parts = toMultiLine(gc);
      if (parts.length === 0) continue;
      features.push({
        type: 'Feature',
        properties: { id: part.id },
        geometry:
          parts.length === 1
            ? { type: 'LineString', coordinates: parts[0] }
            : { type: 'MultiLineString', coordinates: parts },
      });
    }
  }
  return { type: 'FeatureCollection', features };
}

function hasBothEnds(part: PlanPart): boolean {
  return (
    part.start_lat != null &&
    part.start_lon != null &&
    part.end_lat != null &&
    part.end_lon != null
  );
}

function emptyFC(): GeoJSON.FeatureCollection {
  return { type: 'FeatureCollection', features: [] };
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

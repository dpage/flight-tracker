// Great-circle helpers for drawing flight arcs on a map.

type LatLon = [number, number]; // [lon, lat] (GeoJSON convention)

const rad = (d: number) => (d * Math.PI) / 180;
const deg = (r: number) => (r * 180) / Math.PI;

/** Sample N points along the great-circle from a → b, inclusive of both ends. */
export function greatCircle(
  fromLat: number,
  fromLon: number,
  toLat: number,
  toLon: number,
  steps = 64,
): LatLon[] {
  const φ1 = rad(fromLat);
  const λ1 = rad(fromLon);
  const φ2 = rad(toLat);
  const λ2 = rad(toLon);

  const cosΔ = Math.sin(φ1) * Math.sin(φ2) + Math.cos(φ1) * Math.cos(φ2) * Math.cos(λ2 - λ1);
  const Δ = Math.acos(Math.max(-1, Math.min(1, cosΔ)));
  if (Δ < 1e-9) return [[fromLon, fromLat]];

  const out: LatLon[] = [];
  for (let i = 0; i <= steps; i++) {
    const f = i / steps;
    const a = Math.sin((1 - f) * Δ) / Math.sin(Δ);
    const b = Math.sin(f * Δ) / Math.sin(Δ);
    const x = a * Math.cos(φ1) * Math.cos(λ1) + b * Math.cos(φ2) * Math.cos(λ2);
    const y = a * Math.cos(φ1) * Math.sin(λ1) + b * Math.cos(φ2) * Math.sin(λ2);
    const z = a * Math.sin(φ1) + b * Math.sin(φ2);
    const lat = deg(Math.atan2(z, Math.sqrt(x * x + y * y)));
    const lon = deg(Math.atan2(y, x));
    out.push([lon, lat]);
  }
  return splitOnAntimeridian(out);
}

// MapLibre draws GeoJSON LineStrings in screen-space, so a segment that
// crosses ±180° lon would shoot all the way across the map. Insert a break
// (encoded as a MultiLineString — represented here by allowing the consumer
// to chunk on Infinity sentinels) when consecutive longitudes jump by >180°.
function splitOnAntimeridian(coords: LatLon[]): LatLon[] {
  const out: LatLon[] = [];
  for (let i = 0; i < coords.length; i++) {
    if (i > 0) {
      const prev = coords[i - 1];
      const cur = coords[i];
      if (Math.abs(cur[0] - prev[0]) > 180) {
        // Mark a discontinuity by inserting NaN; callers convert to MultiLineString.
        out.push([NaN, NaN]);
      }
    }
    out.push(coords[i]);
  }
  return out;
}

/** Great-circle distance between two points, in statute miles.
 *  Uses the spherical law of cosines on a 3,958.7613-mile Earth radius. */
export function greatCircleMiles(
  fromLat: number,
  fromLon: number,
  toLat: number,
  toLon: number,
): number {
  const φ1 = rad(fromLat);
  const φ2 = rad(toLat);
  const Δλ = rad(toLon - fromLon);
  const cosΔ = Math.sin(φ1) * Math.sin(φ2) + Math.cos(φ1) * Math.cos(φ2) * Math.cos(Δλ);
  const Δ = Math.acos(Math.max(-1, Math.min(1, cosΔ)));
  return Δ * 3958.7613;
}

/**
 * Convert a (possibly antimeridian-split) coord list into a MultiLineString
 * coordinate array suitable for GeoJSON.
 */
export function toMultiLine(coords: LatLon[]): LatLon[][] {
  const parts: LatLon[][] = [[]];
  for (const c of coords) {
    if (Number.isNaN(c[0])) {
      parts.push([]);
    } else {
      parts[parts.length - 1].push(c);
    }
  }
  return parts.filter((p) => p.length > 1);
}

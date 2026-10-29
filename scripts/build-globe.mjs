// Generates the dotted globe on the sign-in page: web/assets/globe.svg holds the
// cacheable land and ocean dots; web/globe.go holds the animated routes and nodes.
//
// Usage: node scripts/build-globe.mjs path/to/ne_50m_land.geojson
// Source: Natural Earth 1:50m land v5.1.2 (public domain),
// https://raw.githubusercontent.com/nvkelso/natural-earth-vector/v5.1.2/geojson/ne_50m_land.geojson
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';

const landSHA256 = 'e874b27a51d146452be360cafb3cc50c86001074a67d534113e6534682f9826b';
const raw = readFileSync(process.argv[2]);
if (createHash('sha256').update(raw).digest('hex') !== landSHA256) throw new Error('unexpected land data checksum');

// View: a 1000-unit square, the sphere centred east of the Asia-Pacific region so
// the region sits on the left part that the sign-in page shows.
const size = 1000, radius = 430, centre = size / 2;
const viewLon = 142, viewLat = 8;
const spacing = 1; // degrees between neighbouring land dots on the sphere
const hiddenRight = 0.62; // the page crops the sphere beyond this share of the radius

const rad = d => d * Math.PI / 180;
const sinLat0 = Math.sin(rad(viewLat)), cosLat0 = Math.cos(rad(viewLat));
function vector(lon, lat) {
  const l = rad(lon), p = rad(lat);
  return [Math.cos(p) * Math.cos(l), Math.cos(p) * Math.sin(l), Math.sin(p)];
}
// Orthographic projection of a point at the given distance from the centre of the sphere.
function project([vx, vy, vz], scale = 1) {
  const lon = Math.atan2(vy, vx), lat = Math.asin(Math.max(-1, Math.min(1, vz / Math.hypot(vx, vy, vz))));
  const d = lon - rad(viewLon);
  const x = Math.cos(lat) * Math.sin(d);
  const y = cosLat0 * Math.sin(lat) - sinLat0 * Math.cos(lat) * Math.cos(d);
  const z = sinLat0 * Math.sin(lat) + cosLat0 * Math.cos(lat) * Math.cos(d);
  return { x: centre + x * radius * scale, y: centre - y * radius * scale, z };
}

// Even-odd point-in-polygon over every land ring; Natural Earth land polygons do not overlap.
const rings = [];
for (const feature of JSON.parse(raw).features) {
  const g = feature.geometry;
  const polygons = g.type === 'Polygon' ? [g.coordinates] : g.coordinates;
  for (const polygon of polygons) {
    for (const ring of polygon) {
      let minX = 180, maxX = -180, minY = 90, maxY = -90;
      for (const [x, y] of ring) { minX = Math.min(minX, x); maxX = Math.max(maxX, x); minY = Math.min(minY, y); maxY = Math.max(maxY, y); }
      rings.push({ ring, minX, maxX, minY, maxY });
    }
  }
}
function isLand(lon, lat) {
  let inside = false;
  for (const r of rings) {
    if (lon < r.minX || lon > r.maxX || lat < r.minY || lat > r.maxY) continue;
    const ring = r.ring;
    for (let i = 0, j = ring.length - 1; i < ring.length; j = i++) {
      const [xi, yi] = ring[i], [xj, yj] = ring[j];
      if ((yi > lat) !== (yj > lat) && lon < (xj - xi) * (lat - yi) / (yj - yi) + xi) inside = !inside;
    }
  }
  return inside;
}

// Dots sit on rings of latitude, evenly spaced along each ring so density is uniform on the sphere.
// Ocean keeps every other dot of every other ring, a coarser lattice aligned with the land.
const layers = { landNear: [], landMid: [], landFar: [], landEdge: [], oceanNear: [], oceanFar: [] };
let row = 0;
for (let lat = -90 + spacing / 2; lat < 90; lat += spacing, row++) {
  const count = Math.max(1, Math.round(360 * Math.cos(rad(lat)) / spacing));
  for (let k = 0; k < count; k++) {
    const lon = -180 + (k + (row % 2) / 2) * 360 / count;
    const p = project(vector(lon, lat));
    if (p.z < 0.1 || p.x > centre + radius * hiddenRight) continue;
    const land = isLand(lon, lat);
    if (!land && (row % 2 || k % 2)) continue;
    if (!land && p.z < 0.22) continue;
    const layer = land ? (p.z > 0.7 ? 'landNear' : p.z > 0.45 ? 'landMid' : p.z > 0.25 ? 'landFar' : 'landEdge') : (p.z > 0.5 ? 'oceanNear' : 'oceanFar');
    layers[layer].push({ ...p, row });
  }
}

// Zero-length subpaths with round caps render as dots; relative moves keep the markup small.
function dots(points) {
  points.sort((a, b) => a.row - b.row || a.x - b.x);
  let d = '', x = 0, y = 0;
  for (const p of points) {
    const px = Math.round(p.x * 10) / 10, py = Math.round(p.y * 10) / 10;
    d += d ? `m${fmt(px - x)} ${fmt(py - y)}h0` : `M${fmt(px)} ${fmt(py)}h0`;
    x = px; y = py;
  }
  return d;
}
function fmt(n) {
  return (Math.round(n * 10) / 10).toString().replace(/^(-?)0\./, '$1.');
}

// Raised great-circle routes from Taipei across the region.
const hub = { lon: 121.5, lat: 25.0 };
const cities = [
  { lon: 139.7, lat: 35.7 }, // Tokyo
  { lon: 127.0, lat: 37.6 }, // Seoul
  { lon: 114.2, lat: 22.3 }, // Hong Kong
  { lon: 103.8, lat: 1.35 }, // Singapore
  { lon: 151.2, lat: -33.9 }, // Sydney
  { lon: 72.9, lat: 19.1 }, // Mumbai
];
function arc(from, to) {
  const a = vector(from.lon, from.lat), b = vector(to.lon, to.lat);
  const angle = Math.acos(a[0] * b[0] + a[1] * b[1] + a[2] * b[2]);
  const lift = Math.min(0.22, angle * 0.32);
  const points = [];
  for (let i = 0; i <= 40; i++) {
    const t = i / 40;
    const s1 = Math.sin((1 - t) * angle) / Math.sin(angle), s2 = Math.sin(t * angle) / Math.sin(angle);
    const v = [a[0] * s1 + b[0] * s2, a[1] * s1 + b[1] * s2, a[2] * s1 + b[2] * s2];
    points.push(project(v, 1 + lift * Math.sin(Math.PI * t)));
  }
  return points.map((p, i) => `${i ? 'L' : 'M'}${fmt(p.x)} ${fmt(p.y)}`).join('');
}
const node = c => { const p = project(vector(c.lon, c.lat)); return `{${JSON.stringify(fmt(p.x))}, ${JSON.stringify(fmt(p.y))}}`; };

// Each layer is referenced by id from the page and coloured through currentColor.
const widths = { oceanFar: 2, oceanNear: 2.4, landEdge: 2.1, landFar: 2.8, landMid: 3.3, landNear: 3.6 };
const ids = { oceanFar: 'ocean-far', oceanNear: 'ocean-near', landEdge: 'land-edge', landFar: 'land-far', landMid: 'land-mid', landNear: 'land-near' };
const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${size} ${size}">
<!-- Generated by scripts/build-globe.mjs from Natural Earth 1:50m land (public domain). -->
${Object.keys(widths).map(k => `<path id="${ids[k]}" fill="none" stroke="currentColor" stroke-linecap="round" stroke-width="${widths[k]}" d="${dots(layers[k])}"/>`).join('\n')}
</svg>
`;

const go = `// Code generated by scripts/build-globe.mjs from Natural Earth 1:50m land (public domain). DO NOT EDIT.

package web

type globePoint struct{ X, Y string }

var globeHub = globePoint${node(hub)}

var globeNodes = []globePoint{${cities.map(node).join(', ')}}

var globeRoutes = []string{${cities.map(c => JSON.stringify(arc(hub, c))).join(', ')}}
`;
writeFileSync(new URL('../web/assets/globe.svg', import.meta.url), svg);
writeFileSync(new URL('../web/globe.go', import.meta.url), go);
console.log(Object.fromEntries(Object.entries(layers).map(([k, v]) => [k, v.length])), `svg ${svg.length} bytes, go ${go.length} bytes`);

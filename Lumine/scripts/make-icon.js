// Renders images/icon.png (256x256) with no dependencies: a woven grid on a rounded square.
// Deterministic, so the icon can be regenerated and reviewed:  node scripts/make-icon.js
const fs = require('fs');
const path = require('path');
const zlib = require('zlib');

const SIZE = 256, SS = 4;                 // 4x4 supersampling for smooth edges
const RADIUS = 52;                        // corner radius of the tile
const BARS = 4, BAR_W = 30, GAP = 20;     // weave geometry
const span = BARS * BAR_W + (BARS - 1) * GAP;
const origin = (SIZE - span) / 2;

const mix = (a, b, t) => a.map((v, i) => v + (b[i] - v) * t);
const BG_TOP = [49, 46, 129], BG_BOT = [17, 24, 39];        // indigo -> near black
const WARP = [129, 140, 248], WEFT = [253, 224, 71];         // indigo-300, amber-300

function inRoundedSquare(x, y) {
  const cx = Math.min(Math.max(x, RADIUS), SIZE - RADIUS);
  const cy = Math.min(Math.max(y, RADIUS), SIZE - RADIUS);
  return (x - cx) ** 2 + (y - cy) ** 2 <= RADIUS ** 2;
}
const barIndex = (v) => {
  const t = v - origin;
  if (t < 0) return -1;
  const i = Math.floor(t / (BAR_W + GAP));
  return i < BARS && t - i * (BAR_W + GAP) <= BAR_W ? i : -1;
};

function sample(x, y) {
  if (!inRoundedSquare(x, y)) return [0, 0, 0, 0];
  const bg = mix(BG_TOP, BG_BOT, y / SIZE);
  const v = barIndex(x), h = barIndex(y);       // v: vertical (warp) bar, h: horizontal (weft) bar
  if (v >= 0 && h >= 0) return [...((v + h) % 2 === 0 ? WARP : WEFT), 255];   // over / under
  if (v >= 0) return [...WARP, 255];
  if (h >= 0) return [...WEFT, 255];
  return [...bg, 255];
}

const raw = Buffer.alloc(SIZE * (SIZE * 4 + 1));
for (let py = 0; py < SIZE; py++) {
  raw[py * (SIZE * 4 + 1)] = 0;                 // filter: none
  for (let px = 0; px < SIZE; px++) {
    let r = 0, g = 0, b = 0, a = 0;
    for (let sy = 0; sy < SS; sy++) for (let sx = 0; sx < SS; sx++) {
      const [sr, sg, sb, sa] = sample(px + (sx + 0.5) / SS, py + (sy + 0.5) / SS);
      r += sr * sa; g += sg * sa; b += sb * sa; a += sa;
    }
    const o = py * (SIZE * 4 + 1) + 1 + px * 4;
    if (a > 0) { raw[o] = Math.round(r / a); raw[o + 1] = Math.round(g / a); raw[o + 2] = Math.round(b / a); }
    raw[o + 3] = Math.round(a / (SS * SS));
  }
}

const crcTable = Array.from({ length: 256 }, (_, n) => {
  let c = n; for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1; return c >>> 0;
});
const crc32 = (buf) => { let c = 0xffffffff; for (const b of buf) c = crcTable[(c ^ b) & 0xff] ^ (c >>> 8); return (c ^ 0xffffffff) >>> 0; };
const chunk = (type, data) => {
  const len = Buffer.alloc(4); len.writeUInt32BE(data.length);
  const td = Buffer.concat([Buffer.from(type), data]);
  const crc = Buffer.alloc(4); crc.writeUInt32BE(crc32(td));
  return Buffer.concat([len, td, crc]);
};
const ihdr = Buffer.alloc(13);
ihdr.writeUInt32BE(SIZE, 0); ihdr.writeUInt32BE(SIZE, 4); ihdr[8] = 8; ihdr[9] = 6;   // 8-bit RGBA
const png = Buffer.concat([
  Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
  chunk('IHDR', ihdr), chunk('IDAT', zlib.deflateSync(raw, { level: 9 })), chunk('IEND', Buffer.alloc(0)),
]);
const out = path.join(__dirname, '..', 'images', 'icon.png');
fs.writeFileSync(out, png);
console.log(`wrote ${path.relative(process.cwd(), out)} (${png.length} bytes)`);

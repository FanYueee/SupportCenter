import optimise, { init as initPng } from '@jsquash/oxipng/optimise';
import encode, { init as initWebp } from '@jsquash/webp/encode';

let pngReady;
let webpReady;

function pngCodec() {
  return pngReady ??= initPng('/assets/oxipng.wasm');
}

function webpCodec() {
  return webpReady ??= initWebp({
    locateFile: name => '/assets/' + (name.includes('simd') ? 'webp_enc_simd.wasm' : 'webp_enc.wasm'),
  });
}

function animatedPNG(bytes) {
  for (let pos = 8; pos + 12 <= bytes.length && pos < 65536;) {
    const size = new DataView(bytes.buffer, bytes.byteOffset + pos, 4).getUint32(0);
    const type = String.fromCharCode(...bytes.subarray(pos + 4, pos + 8));
    if (type === 'acTL') return true;
    if (type === 'IDAT' || size > bytes.length - pos - 12) return false;
    pos += size + 12;
  }
  return false;
}

self.onmessage = async ({ data }) => {
  const { file, pixels, width, height } = data;
  try {
    let best = null;
    let bestType = '';
    let bestSize = file.size;
    if (file.type === 'image/png') {
      const original = await file.arrayBuffer();
      if (!animatedPNG(new Uint8Array(original))) {
        await pngCodec();
        const png = await optimise(original, { level: 6, optimiseAlpha: false });
        if (png.byteLength < bestSize) {
          best = png;
          bestType = 'image/png';
          bestSize = png.byteLength;
        }
      }
    }
    await webpCodec();
    // All pixel values are retained. Never replace an animated WebP's frames.
    const webp = await encode({ data: pixels, width, height }, {
      lossless: 1, quality: 100, method: 6, exact: 1, near_lossless: 100,
    });
    if (file.type !== 'image/webp' && webp.byteLength < bestSize) {
      best = webp;
      bestType = 'image/webp';
    }
    self.postMessage({ best, bestType, preview: webp },
      best && best !== webp ? [best, webp] : [webp]);
  } catch (error) {
    self.postMessage({ error: error instanceof Error ? error.message : '圖片處理失敗' });
  }
};

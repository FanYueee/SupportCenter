import { build } from 'esbuild';
import { copyFileSync, readdirSync, rmSync } from 'node:fs';

const out = 'web/assets';
for (const name of readdirSync(out)) {
  if (name === 'image-worker.js' || /^codec-[A-Z0-9]+\.js$/.test(name)) rmSync(`${out}/${name}`);
}
await build({
  entryPoints: ['web/image-worker.js'],
  outdir: out,
  entryNames: 'image-worker',
  chunkNames: 'codec-[hash]',
  bundle: true,
  splitting: true,
  format: 'esm',
  target: 'es2022',
  minify: true,
});
copyFileSync('node_modules/@jsquash/oxipng/codec/pkg/squoosh_oxipng_bg.wasm', `${out}/oxipng.wasm`);
copyFileSync('node_modules/@jsquash/webp/codec/enc/webp_enc.wasm', `${out}/webp_enc.wasm`);
copyFileSync('node_modules/@jsquash/webp/codec/enc/webp_enc_simd.wasm', `${out}/webp_enc_simd.wasm`);

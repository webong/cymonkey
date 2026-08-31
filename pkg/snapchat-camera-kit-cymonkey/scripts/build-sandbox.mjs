import { build } from 'esbuild';

await build({
  entryPoints: ['src/sandbox.ts'],
  bundle: true,
  format: 'esm',
  platform: 'browser',
  target: 'es2022',
  minify: true,
  legalComments: 'none',
  outfile: 'dist/sandbox.js',
});

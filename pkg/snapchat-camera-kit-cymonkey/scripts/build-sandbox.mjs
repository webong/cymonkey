import { build } from 'esbuild';

await build({
  entryPoints: ['src/sandbox.ts'],
  bundle: true,
  format: 'esm',
  platform: 'browser',
  target: 'es2022',
  legalComments: 'none',
  outfile: 'dist/sandbox.js',
});

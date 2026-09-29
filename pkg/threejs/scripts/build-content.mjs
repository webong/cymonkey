import {build} from 'esbuild';

await build({
  entryPoints: ['src/content.ts'],
  bundle: true,
  format: 'iife',
  platform: 'browser',
  target: 'es2022',
  minify: true,
  legalComments: 'none',
  outfile: 'dist/content.js',
});

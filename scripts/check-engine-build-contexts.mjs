// Validate local COPY inputs without downloading or running licensed engine images.
import { readFile, access } from 'node:fs/promises';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';

export const engineContainers = [
  'godot-cymonkey', 'godot-cymonkey/godot-cymonkey-gpu',
  'unity-cymonkey', 'unity-cymonkey/unity-cymonkey-gpu',
  'unreal-cymonkey', 'unreal-cymonkey/unreal-cymonkey-gpu',
];
const root = fileURLToPath(new URL('../', import.meta.url));

export function localCopySources(container) {
  const sources = [];
  for (const line of container.split('\n')) {
    if (!/^COPY\s/i.test(line)) continue;
    if (/^COPY\s+--from=/i.test(line)) continue; // Build-stage outputs, not repository inputs.
    const args = line.trim().split(/\s+/).slice(1);
    // These fixtures use simple shell-form COPY; fail closed if that changes.
    if (args.length < 2 || args.some((arg) => !/^[\w./-]+$/.test(arg)
      || arg.startsWith('--') || arg.split('/').includes('..'))) {
      throw new Error(`Unsupported COPY syntax: ${line}`);
    }
    const inputs = args.slice(0, -1);
    if (inputs.some((arg) => arg.startsWith('/'))) throw new Error(`Non-relative COPY source: ${line}`);
    sources.push(...inputs);
  }
  if (!sources.length) throw new Error('Containerfile has no local COPY inputs');
  return sources;
}

export async function engineCopyInputs() {
  const inputs = new Set();
  for (const engine of engineContainers) {
    const container = await readFile(resolve(root, `scripts/deploy/${engine}/Containerfile`), 'utf8');
    for (const source of localCopySources(container)) {
      await access(resolve(root, source));
      inputs.add(source);
    }
  }
  return [...inputs].sort();
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const sources = await engineCopyInputs();
  const dockerfile = ['FROM scratch', ...sources.map((source, i) => `COPY ${source} /validated/${i}/`)];
  const result = spawnSync('docker', ['buildx', 'build', '--progress=plain', '--output=type=cacheonly', '-f', '-', '.'], {
    cwd: root, input: dockerfile.join('\n') + '\n', stdio: ['pipe', 'inherit', 'inherit'], timeout: 120_000,
  });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`Docker context build failed: ${result.signal ?? result.status}`);
  console.log(`Validated ${sources.length} distinct local COPY inputs across ${engineContainers.length} engine Containerfiles. No engine code was built or run.`);
}

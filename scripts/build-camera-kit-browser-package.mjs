import {cp, mkdir, readFile, stat, writeFile} from 'node:fs/promises';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
const targets = ['chrome-mv3', 'edge-mv3'];
const cameraKit = `${root}pkg/snapchat-camera-kit-cymonkey`;
const extension = `${root}pkg/browser-ext`;

run('npm', ['--prefix', cameraKit, 'run', 'build:sandbox']);
run('npm', ['--prefix', extension, 'run', 'build:chrome']);
run('npm', ['--prefix', extension, 'run', 'build:edge']);

for (const target of targets) {
  const output = `${extension}/.output/${target}`;
  const destination = `${output}/augmentations/snapchat-camera-kit/sandbox.js`;
  await mkdir(`${output}/augmentations/snapchat-camera-kit`, {recursive: true});
  await cp(`${cameraKit}/dist/sandbox.js`, destination);
  const manifestPath = `${output}/manifest.json`;
  const manifest = JSON.parse(await readFile(manifestPath, 'utf8'));
  if (!manifest.sandbox?.pages?.includes('runtime-sandbox.html')) throw new Error(`${target} does not provide the generic Jangolova sandbox`);
  manifest.content_security_policy = {
    ...(manifest.content_security_policy || {}),
    sandbox: "sandbox allow-scripts; script-src 'self' https://cf-st.sc-cdn.net blob: 'wasm-unsafe-eval'; connect-src 'self' https://*.snapar.com; object-src 'none'; base-uri 'none';",
  };
  await writeFile(manifestPath, `${JSON.stringify(manifest)}\n`);
  if ((await stat(destination)).size < 100_000) throw new Error(`${target} Camera Kit sandbox bundle is unexpectedly small`);
  process.stdout.write(`composed Camera Kit sandbox for ${target}\n`);
}

function run(command, args) {
  const result = spawnSync(command, args, {cwd: root, stdio: 'inherit'});
  if (result.status !== 0) throw new Error(`${command} ${args.join(' ')} failed`);
}

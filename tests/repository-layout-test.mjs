import assert from 'node:assert/strict';
import { access, readFile, readdir } from 'node:fs/promises';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { engineCopyInputs, localCopySources } from '../scripts/check-engine-build-contexts.mjs';

const root = fileURLToPath(new URL('../', import.meta.url));
const read = (path) => readFile(resolve(root, path), 'utf8');
const exists = async (path) => {
  await assert.doesNotReject(access(resolve(root, path)), `missing repository path: ${path}`);
};

test('all affected engine Containerfile local COPY inputs exist, including runner scripts', async () => {
  const sources = await engineCopyInputs();
  assert.equal(sources.filter((source) => source.endsWith('/run-fixture.sh')).length, 5);
});

test('COPY input extraction excludes stage outputs and rejects unsupported syntax', () => {
  assert.deepEqual(localCopySources('COPY pkg/godot tests/godot-cymonkey-fixture /workspace/\nCOPY --from=builder /out /app'),
    ['pkg/godot', 'tests/godot-cymonkey-fixture']);
  for (const invalid of ['COPY ["pkg/godot", "/app"]', 'COPY $SOURCE /app', 'COPY ../outside /app', 'COPY /absolute /app']) {
    assert.throws(() => localCopySources(invalid));
  }
});

test('npm script package paths resolve without requiring installed dependencies', async () => {
  const { scripts } = JSON.parse(await read('package.json'));
  for (const command of Object.values(scripts)) {
    for (const [path] of command.matchAll(/\bpkg\/[\w./-]+/g)) {
      // Dependency executables are installed separately; their package must exist.
      await exists(path.split('/node_modules/')[0]);
    }
  }
});

test('engine container package sources and Unity local dependencies exist', async () => {
  let checked = 0;
  async function checkContainers(base) {
    const entries = await readdir(resolve(root, base), { withFileTypes: true });
    for (const entry of entries) {
      const path = `${base}/${entry.name}`;
      if (entry.isDirectory()) {
        await checkContainers(path);
      } else if (entry.name.endsWith('Containerfile')) {
        const container = await read(path);
        for (const [, source] of container.matchAll(/^COPY\s+(pkg\/\S+)\s/gm)) {
          await exists(source);
          checked++;
        }
      }
    }
  }
  await checkContainers('scripts/deploy');
  assert.ok(checked > 0, 'expected package COPY sources');
  const manifest = 'tests/unity-cymonkey-fixture/Packages/manifest.json';
  for (const dependency of Object.values(JSON.parse(await read(manifest)).dependencies)) {
    if (dependency.startsWith('file:')) {
      await exists(resolve(root, dirname(manifest), dependency.slice(5), 'package.json'));
    }
  }
});

test('macOS helper exposes native and browser host APIs from one Swift package', async () => {
  const manifest = await read('pkg/macos-helper/Package.swift');
  assert.match(manifest, /\.library\(name: "CymonkeyMacOSRuntime"/);
  assert.match(manifest, /\.executable\(name: "cymonkey-macos-helper"/);
  for (const file of ['ManagedRuntime.swift', 'BrowserModels.swift', 'UserscriptCatalog.swift']) {
    await exists(`pkg/macos-helper/Sources/CymonkeyMacOSRuntime/${file}`);
  }
});

test('skill folders, declared names, invocations, and reference links agree', async () => {
  const skills = await readdir(resolve(root, '.agents/skills'), { withFileTypes: true });
  assert.ok(skills.some((entry) => entry.isDirectory()), 'expected repository skills');
  for (const skill of skills.filter((entry) => entry.isDirectory())) {
    const base = `.agents/skills/${skill.name}`;
    const source = await read(`${base}/SKILL.md`);
    const frontmatter = source.match(/^---\n([\s\S]*?)\n---/)?.[1];
    assert.ok(frontmatter, `${base}: missing frontmatter`);
    assert.equal(frontmatter.match(/^name: (.+)$/m)?.[1], skill.name);
    const metadata = await read(`${base}/agents/openai.yaml`);
    const prompt = metadata.match(/^\s*default_prompt: "(.+)"$/m)?.[1];
    assert.ok(prompt?.includes(`$${skill.name} `), `${base}: invocation must use its declared name`);
    for (const [, reference] of source.matchAll(/\]\((references\/[^)]+)\)/g)) {
      await exists(`${base}/${reference}`);
    }
  }
});

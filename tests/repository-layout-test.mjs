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
  const directories = await readdir(resolve(root, 'infra/deploy'), { withFileTypes: true });
  let checked = 0;
  for (const directory of directories.filter((entry) => entry.isDirectory())) {
    const base = `infra/deploy/${directory.name}`;
    for (const name of (await readdir(resolve(root, base))).filter((file) => file.endsWith('Containerfile'))) {
      const container = await read(`${base}/${name}`);
      for (const [, path] of container.matchAll(/^COPY\s+(pkg\/\S+)\s/gm)) {
        await exists(path);
        checked++;
      }
    }
  }
  assert.ok(checked > 0, 'expected package COPY sources');
  const manifest = 'tests/unity-cymonkey-fixture/Packages/manifest.json';
  for (const dependency of Object.values(JSON.parse(await read(manifest)).dependencies)) {
    if (dependency.startsWith('file:')) {
      await exists(resolve(root, dirname(manifest), dependency.slice(5), 'package.json'));
    }
  }
});

test('macOS adapter local dependency path and product identity agree', async () => {
  const manifest = await read('pkg/macos-browser-adapter/Package.swift');
  const dependency = manifest.match(/\.package\(path: "([^"]+)"\)/)?.[1];
  assert.ok(dependency, 'expected a local runtime dependency');
  await exists(resolve(root, 'pkg/macos-browser-adapter', dependency, 'Package.swift'));
  const identity = dependency.split('/').at(-1);
  assert.ok(manifest.includes(`package: "${identity}"`), 'Swift product must use the local package identity');
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

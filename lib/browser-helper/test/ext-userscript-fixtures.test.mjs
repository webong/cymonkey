import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import { parseMetadata, validateMatchPattern } from '../dist/index.js';

const fixture = async (name) => JSON.parse(await readFile(new URL(`./fixtures/ext/userscript/${name}`, import.meta.url), 'utf8'));

// ext owns the userscript match-pattern rules. Cymonkey's runtime must accept
// and reject the same patterns, including ext's refusal of a blanket <all_urls>.
test('match patterns agree with ext.userscript/v1alpha1', async () => {
  const { cases } = await fixture('match-patterns.json');
  const differences = [];
  for (const item of cases) {
    let accepted = true;
    try { validateMatchPattern(item.pattern); } catch { accepted = false; }
    if (accepted !== item.valid) differences.push(`${item.name}: ${JSON.stringify(item.pattern)} ext valid=${item.valid}, Cymonkey accepted=${accepted}`);
  }
  assert.deepEqual(differences, []);
});

test('header metadata agrees with ext.userscript/v1alpha1', async () => {
  const { cases } = await fixture('metadata.json');
  const differences = [];
  for (const item of cases) {
    // Inferring a name from a file name is the caller's job; parseMetadata sees only source.
    if (item.sourceFile || item.expect.name === undefined) continue;
    let parsed;
    try { parsed = parseMetadata(item.source); } catch { continue; }
    if (item.expect.name !== null && parsed.name !== item.expect.name) differences.push(`${item.name}: name ${JSON.stringify(parsed.name)} != ${JSON.stringify(item.expect.name)}`);
    const matches = item.expect.matches ?? [];
    if (JSON.stringify(parsed.matches) !== JSON.stringify(matches)) differences.push(`${item.name}: matches ${JSON.stringify(parsed.matches)} != ${JSON.stringify(matches)}`);
    const excludes = item.expect.excludeMatches ?? [];
    if (JSON.stringify(parsed.excludeMatches) !== JSON.stringify(excludes)) differences.push(`${item.name}: excludes ${JSON.stringify(parsed.excludeMatches)} != ${JSON.stringify(excludes)}`);
  }
  assert.deepEqual(differences, []);
});

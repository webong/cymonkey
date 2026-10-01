import type { ExecutionWorld, ParsedMetadata, RunAt } from './types.js';

const startMarker = '// ==UserScript==';
const endMarker = '// ==/UserScript==';
const maxMetadataBytes = 64 * 1024;

export function parseMetadata(source: string): ParsedMetadata {
  const header = source.slice(0, maxMetadataBytes);
  const start = header.indexOf(startMarker);
  const end = header.indexOf(endMarker, start + startMarker.length);
  if (start < 0 || end < 0) throw new Error('userscript metadata block is required near the beginning of source');
  const values = new Map<string, string[]>();
  for (const line of header.slice(start + startMarker.length, end).split(/\r?\n/)) {
    const match = /^\s*\/\/\s*@([A-Za-z][A-Za-z0-9_-]*)\s+(.+?)\s*$/.exec(line);
    if (!match) continue;
    const key = match[1]!.toLowerCase();
    values.set(key, [...(values.get(key) ?? []), match[2]!]);
  }
  return {
    name: first(values, 'name'),
    namespace: first(values, 'namespace'),
    version: first(values, 'version'),
    description: first(values, 'description'),
    matches: values.get('match') ?? [],
    excludeMatches: values.get('exclude-match') ?? [],
    grants: values.get('grant') ?? ['none'],
    runAt: normalizeRunAt(first(values, 'run-at')),
    world: normalizeWorld(first(values, 'inject-into')),
    updateUrl: first(values, 'updateurl') ?? first(values, 'downloadurl'),
  };
}

function first(values: Map<string, string[]>, key: string) {
  return values.get(key)?.[0];
}

function normalizeRunAt(value?: string): RunAt | undefined {
  if (!value) return undefined;
  const normalized = value.replaceAll('-', '_');
  if (normalized === 'document_start' || normalized === 'document_end' || normalized === 'document_idle') return normalized;
  throw new Error(`unsupported userscript @run-at ${JSON.stringify(value)}`);
}

function normalizeWorld(value?: string): ExecutionWorld | undefined {
  if (!value || value === 'content') return 'USER_SCRIPT';
  if (value === 'page') return 'MAIN';
  throw new Error(`unsupported userscript @inject-into ${JSON.stringify(value)}`);
}

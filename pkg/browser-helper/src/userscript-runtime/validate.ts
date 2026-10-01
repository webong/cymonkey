import { parseMetadata } from './metadata.js';
import { userscriptProtocolVersion, type UserScriptManifest } from './types.js';

const idPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const revisionPattern = /^sha256:[a-f0-9]{64}$/;
const maxSourceBytes = 1024 * 1024;

export function validateManifest(value: UserScriptManifest, options: {allowMainWorld?: boolean} = {}): UserScriptManifest {
  if (value.apiVersion !== userscriptProtocolVersion || value.kind !== 'UserScript') throw new Error('unsupported userscript contract');
  if (!idPattern.test(value.metadata.id)) throw new Error('invalid userscript id');
  if (!revisionPattern.test(value.metadata.revision)) throw new Error('userscript revision must be a sha256 digest');
  if (!value.metadata.name || value.metadata.name.length > 128) throw new Error('invalid userscript name');
  if (!value.source.code || new TextEncoder().encode(value.source.code).byteLength > maxSourceBytes) throw new Error('userscript source exceeds the size limit');
  validateOrigin(value.source.origin, false);
  if (value.spec.updateUrl) validateOrigin(value.spec.updateUrl, true);
  if (value.spec.matches.length === 0 || value.spec.matches.length > 128) throw new Error('userscript requires bounded match patterns');
  for (const pattern of [...value.spec.matches, ...value.spec.excludeMatches]) validateMatchPattern(pattern);
  if (value.spec.grants.length !== 1 || value.spec.grants[0] !== 'none') throw new Error('userscript MVP supports @grant none only');
  if (value.spec.world === 'MAIN' && !options.allowMainWorld) throw new Error('MAIN world userscripts require explicit approval');
  const metadata = parseMetadata(value.source.code);
  if (metadata.name && metadata.name !== value.metadata.name) throw new Error('userscript manifest name does not match source metadata');
  if (!sameStrings(metadata.matches, value.spec.matches)) throw new Error('userscript matches do not match source metadata');
  if (!sameStrings(metadata.excludeMatches, value.spec.excludeMatches)) throw new Error('userscript excludes do not match source metadata');
  if (!sameStrings(stable(metadata.grants), value.spec.grants)) throw new Error('userscript grants do not match source metadata');
  return structuredClone(value);
}

export function validateMatchPattern(value: string) {
  if (value === '<all_urls>') return;
  const match = /^(\*|https?|file):\/\/([^/]+|\*)\/(.*)$/.exec(value);
  if (!match) throw new Error(`invalid userscript match pattern ${JSON.stringify(value)}`);
  if (match[1] === 'file' && match[2] !== '*') throw new Error('file userscript match host must be *');
  if (value.length > 512) throw new Error('userscript match pattern is too long');
}

function validateOrigin(value: string, remoteRequired: boolean) {
  if (!remoteRequired && value === 'user') return;
  let parsed: URL;
  try { parsed = new URL(value); } catch { throw new Error('invalid userscript source URL'); }
  if (parsed.protocol !== 'https:') throw new Error('remote userscript URLs must use HTTPS');
  if (parsed.username || parsed.password) throw new Error('userscript URLs cannot contain credentials');
}

function stable(values: string[]) {
  return [...new Set(values)].sort();
}

function sameStrings(left: string[], right: string[]) {
  return JSON.stringify(stable(left)) === JSON.stringify(stable(right));
}

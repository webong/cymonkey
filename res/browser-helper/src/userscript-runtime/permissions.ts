import type { PermissionIncrease, UserScriptManifest } from './types.js';

export function permissionIncrease(previous: UserScriptManifest, next: UserScriptManifest): PermissionIncrease {
  return {
    addedMatches: difference(next.spec.matches, previous.spec.matches),
    addedGrants: difference(next.spec.grants, previous.spec.grants),
    mainWorldAdded: previous.spec.world !== 'MAIN' && next.spec.world === 'MAIN',
    updateOriginChanged: origin(previous.spec.updateUrl) !== origin(next.spec.updateUrl),
  };
}

export function requiresRenewedApproval(value: PermissionIncrease) {
  return value.addedMatches.length > 0 || value.addedGrants.length > 0 || value.mainWorldAdded || value.updateOriginChanged;
}

function difference(next: string[], previous: string[]) {
  const known = new Set(previous);
  return [...new Set(next)].filter((value) => !known.has(value)).sort();
}

function origin(value?: string) {
  return value ? new URL(value).origin : '';
}

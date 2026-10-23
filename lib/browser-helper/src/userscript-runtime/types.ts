export const userscriptProtocolVersion = 'cymonkey.userscript/v1alpha1' as const;

export type RunAt = 'document_start' | 'document_end' | 'document_idle';
export type ExecutionWorld = 'USER_SCRIPT' | 'MAIN';

export type UserScriptManifest = {
  apiVersion: typeof userscriptProtocolVersion;
  kind: 'UserScript';
  metadata: {
    id: string;
    revision: `sha256:${string}`;
    name: string;
    namespace?: string;
    version?: string;
    description?: string;
  };
  spec: {
    matches: string[];
    excludeMatches: string[];
    runAt: RunAt;
    world: ExecutionWorld;
    allFrames: boolean;
    grants: string[];
    enabled: boolean;
    updateUrl?: string;
  };
  source: {
    origin: string;
    code: string;
  };
};

export type ParsedMetadata = {
  name?: string;
  namespace?: string;
  version?: string;
  description?: string;
  matches: string[];
  excludeMatches: string[];
  grants: string[];
  runAt?: RunAt;
  world?: ExecutionWorld;
  updateUrl?: string;
};

export type PermissionIncrease = {
  addedMatches: string[];
  addedGrants: string[];
  mainWorldAdded: boolean;
  updateOriginChanged: boolean;
};

export type BrowserRegistration = {
  id: string;
  matches: string[];
  excludeMatches?: string[];
  js: Array<{code: string}>;
  runAt: RunAt;
  world: ExecutionWorld;
  worldId?: string;
  allFrames: boolean;
};

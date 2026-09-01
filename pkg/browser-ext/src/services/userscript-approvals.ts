import { publicDescription, type UserScriptManifest } from '@jangolova/userscript-runtime';
import { appendEvent } from './events';
import { requireScopedIdentifier } from './policy';

export type UserscriptApproval = {
  kind: 'userscript';
  id: string;
  status: 'pending' | 'approved' | 'denied';
  createdAt: string;
  expiresAt: string;
  operation: 'install' | 'update';
  scriptId: string;
  scriptName: string;
  scriptNamespace?: string;
  scriptVersion?: string;
  scriptDescription?: string;
  revision: string;
  matches: string[];
  excludeMatches: string[];
  world: 'USER_SCRIPT' | 'MAIN';
  runAt: string;
  grants: string[];
  sourceOrigin: string;
  sourceBytes: number;
  updateUrl?: string;
};

const storageKey = 'jangolova.userscriptApprovals.v1';
const lifetimeMilliseconds = 5 * 60 * 1000;

function approvalStorage() {
  return browser.storage.session ?? browser.storage.local;
}

export async function authorizeUserscriptMutation(operation: 'install' | 'update', manifest: UserScriptManifest, approvalId: unknown) {
  const approvals = await readApprovals();
  const requested = approvalFor(operation, manifest);
  const id = typeof approvalId === 'string' ? requireScopedIdentifier(approvalId, 'approval id') : null;
  if (id) {
    const index = approvals.findIndex((item) => item.id === id);
    const approval = approvals[index];
    if (!approval || !sameRequest(approval, requested)) throw new Error('userscript approval does not match this script revision');
    if (approval.status === 'denied') throw new Error('userscript installation was denied by the user');
    if (approval.status === 'pending') return {approved: false as const, approval: publicApproval(approval)};
    approvals.splice(index, 1);
    await writeApprovals(approvals);
    await appendEvent('approval.userscript.consumed', approvalEvent(approval));
    return {approved: true as const};
  }
  const existing = approvals.find((item) => item.status === 'pending' && sameRequest(item, requested));
  if (existing) return {approved: false as const, approval: publicApproval(existing)};
  const createdAt = new Date();
  const approval: UserscriptApproval = {
    ...requested,
    id: `userscript-approval-${randomID()}`,
    status: 'pending',
    createdAt: createdAt.toISOString(),
    expiresAt: new Date(createdAt.getTime() + lifetimeMilliseconds).toISOString(),
  };
  approvals.push(approval);
  await writeApprovals(approvals);
  await appendEvent('approval.userscript.requested', approvalEvent(approval));
  return {approved: false as const, approval: publicApproval(approval)};
}

export async function listUserscriptApprovals() {
  const approvals = await readApprovals();
  await writeApprovals(approvals);
  return approvals.filter((item) => item.status === 'pending').map(publicApproval);
}

export async function resolveUserscriptApproval(idValue: unknown, decision: unknown) {
  const id = requireScopedIdentifier(idValue, 'approval id');
  if (decision !== 'approve' && decision !== 'deny') throw new Error('approval decision must be approve or deny');
  const approvals = await readApprovals();
  const approval = approvals.find((item) => item.id === id);
  if (!approval || approval.status !== 'pending') throw new Error('userscript approval is no longer pending');
  approval.status = decision === 'approve' ? 'approved' : 'denied';
  await writeApprovals(approvals);
  await appendEvent(`approval.userscript.${approval.status}`, approvalEvent(approval));
  return {ok: true, id, status: approval.status};
}

function approvalFor(operation: 'install' | 'update', manifest: UserScriptManifest): Omit<UserscriptApproval, 'id' | 'status' | 'createdAt' | 'expiresAt'> {
  const description = publicDescription(manifest);
  return {
    kind: 'userscript', operation,
    scriptId: description.metadata.id,
    scriptName: description.metadata.name,
    ...(description.metadata.namespace ? {scriptNamespace: description.metadata.namespace} : {}),
    ...(description.metadata.version ? {scriptVersion: description.metadata.version} : {}),
    ...(description.metadata.description ? {scriptDescription: description.metadata.description} : {}),
    revision: description.metadata.revision,
    matches: [...description.spec.matches],
    excludeMatches: [...description.spec.excludeMatches],
    world: description.spec.world,
    runAt: description.spec.runAt,
    grants: [...description.spec.grants],
    sourceOrigin: description.source.origin,
    sourceBytes: description.source.bytes,
    ...(description.spec.updateUrl ? {updateUrl: description.spec.updateUrl} : {}),
  };
}

async function readApprovals(): Promise<UserscriptApproval[]> {
  const stored = await approvalStorage().get(storageKey);
  const raw = Array.isArray(stored[storageKey]) ? stored[storageKey] as unknown[] : [];
  const approvals = raw.filter((item): item is UserscriptApproval => validApproval(item) && Date.parse(item.expiresAt) > Date.now());
  if (raw.length !== approvals.length) await writeApprovals(approvals);
  return approvals;
}

async function writeApprovals(approvals: UserscriptApproval[]) {
  await approvalStorage().set({[storageKey]: approvals});
}

function sameRequest(left: UserscriptApproval, right: Omit<UserscriptApproval, 'id' | 'status' | 'createdAt' | 'expiresAt'>) {
  return left.operation === right.operation && left.scriptId === right.scriptId && left.revision === right.revision
    && left.matches.join('\u0000') === right.matches.join('\u0000')
    && left.excludeMatches.join('\u0000') === right.excludeMatches.join('\u0000')
    && left.world === right.world && left.runAt === right.runAt
    && left.grants.join('\u0000') === right.grants.join('\u0000') && left.sourceOrigin === right.sourceOrigin;
}

function publicApproval(approval: UserscriptApproval) {
  return structuredClone(approval);
}

function approvalEvent(approval: UserscriptApproval) {
  const {id, status, createdAt, expiresAt, ...data} = approval;
  return {approvalId: id, ...data};
}

function validApproval(value: unknown): value is UserscriptApproval {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false;
  const item = value as Partial<UserscriptApproval>;
  return item.kind === 'userscript' && typeof item.id === 'string'
    && (item.status === 'pending' || item.status === 'approved' || item.status === 'denied')
    && typeof item.createdAt === 'string' && typeof item.expiresAt === 'string'
    && (item.operation === 'install' || item.operation === 'update')
    && typeof item.scriptId === 'string' && typeof item.scriptName === 'string' && typeof item.revision === 'string'
    && (item.scriptNamespace === undefined || typeof item.scriptNamespace === 'string')
    && (item.scriptVersion === undefined || typeof item.scriptVersion === 'string')
    && (item.scriptDescription === undefined || typeof item.scriptDescription === 'string')
    && Array.isArray(item.matches) && item.matches.every((value) => typeof value === 'string')
    && Array.isArray(item.excludeMatches) && item.excludeMatches.every((value) => typeof value === 'string')
    && (item.world === 'USER_SCRIPT' || item.world === 'MAIN') && typeof item.runAt === 'string'
    && Array.isArray(item.grants) && item.grants.every((value) => typeof value === 'string')
    && typeof item.sourceOrigin === 'string' && Number.isInteger(item.sourceBytes)
    && (item.updateUrl === undefined || typeof item.updateUrl === 'string');
}

function randomID() {
  const values = crypto.getRandomValues(new Uint32Array(4));
  return [...values].map((value) => value.toString(16).padStart(8, '0')).join('');
}

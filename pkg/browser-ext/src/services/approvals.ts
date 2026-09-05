import { appendEvent } from './events';
import { requireScopedIdentifier } from './policy';

export type PackageApproval = {
  kind: 'package';
  id: string;
  status: 'pending' | 'approved' | 'denied';
  createdAt: string;
  expiresAt: string;
  packageId: string;
  packageName: string;
  permissions: string[];
  augmentationId: string;
  tabId: number;
  origin: string;
};

type MountApprovalInput = Omit<PackageApproval, 'kind' | 'id' | 'status' | 'createdAt' | 'expiresAt'> & {approvalId?: unknown};

const storageKey = 'cymonkey.packageApprovals.v1';
const lifetimeMilliseconds = 5 * 60 * 1000;

function approvalStorage() {
  return browser.storage.session ?? browser.storage.local;
}

export async function authorizePackageMount(input: MountApprovalInput) {
  const approvals = await readApprovals();
  const approvalId = typeof input.approvalId === 'string' ? requireScopedIdentifier(input.approvalId, 'approval id') : null;
  if (approvalId) {
    const index = approvals.findIndex((item) => item.id === approvalId);
    const approval = approvals[index];
    if (!approval || !sameRequest(approval, input)) throw new Error('package approval does not match this mount request');
    if (approval.status === 'denied') throw new Error('package mount was denied by the user');
    if (approval.status === 'pending') return {approved: false as const, approval: publicApproval(approval)};
    approvals.splice(index, 1);
    await writeApprovals(approvals);
    await appendEvent('approval.package.consumed', approvalEvent(approval));
    return {approved: true as const};
  }

  const existing = approvals.find((item) => item.status === 'pending' && sameRequest(item, input));
  if (existing) return {approved: false as const, approval: publicApproval(existing)};
  const createdAt = new Date();
  const approval: PackageApproval = {
    kind: 'package',
    id: `approval-${randomID()}`,
    status: 'pending',
    createdAt: createdAt.toISOString(),
    expiresAt: new Date(createdAt.getTime() + lifetimeMilliseconds).toISOString(),
    packageId: input.packageId,
    packageName: input.packageName,
    permissions: [...input.permissions],
    augmentationId: input.augmentationId,
    tabId: input.tabId,
    origin: input.origin,
  };
  approvals.push(approval);
  await writeApprovals(approvals);
  await appendEvent('approval.package.requested', approvalEvent(approval));
  return {approved: false as const, approval: publicApproval(approval)};
}

export async function listPackageApprovals() {
  const approvals = await readApprovals();
  await writeApprovals(approvals);
  return approvals.filter((item) => item.status === 'pending').map(publicApproval);
}

export async function resolvePackageApproval(idValue: unknown, decision: unknown) {
  const id = requireScopedIdentifier(idValue, 'approval id');
  if (decision !== 'approve' && decision !== 'deny') throw new Error('approval decision must be approve or deny');
  const approvals = await readApprovals();
  const approval = approvals.find((item) => item.id === id);
  if (!approval || approval.status !== 'pending') throw new Error('package approval is no longer pending');
  approval.status = decision === 'approve' ? 'approved' : 'denied';
  await writeApprovals(approvals);
  await appendEvent(`approval.package.${approval.status}`, approvalEvent(approval));
  return {ok: true, id, status: approval.status};
}

async function readApprovals(): Promise<PackageApproval[]> {
  const stored = await approvalStorage().get(storageKey);
  const now = Date.now();
  const raw = Array.isArray(stored[storageKey]) ? stored[storageKey] as unknown[] : [];
  const approvals = raw.filter((item): item is PackageApproval => validApproval(item) && Date.parse(item.expiresAt) > now);
  if (raw.length !== approvals.length) await writeApprovals(approvals);
  return approvals;
}

async function writeApprovals(approvals: PackageApproval[]) {
  await approvalStorage().set({[storageKey]: approvals});
  await browser.action.setBadgeBackgroundColor({color: '#d97706'}).catch(() => undefined);
  const pendingCount = approvals.filter((item) => item.status === 'pending').length;
  await browser.action.setBadgeText({text: pendingCount ? String(pendingCount) : ''}).catch(() => undefined);
}

function sameRequest(approval: PackageApproval, input: MountApprovalInput) {
  return approval.packageId === input.packageId
    && approval.augmentationId === input.augmentationId
    && approval.tabId === input.tabId
    && approval.origin === input.origin
    && approval.permissions.join('\u0000') === input.permissions.join('\u0000');
}

function publicApproval(approval: PackageApproval) {
  return {
    id: approval.id, status: approval.status, createdAt: approval.createdAt, expiresAt: approval.expiresAt,
    packageId: approval.packageId, packageName: approval.packageName, permissions: [...approval.permissions],
    augmentationId: approval.augmentationId, tabId: approval.tabId, origin: approval.origin,
  };
}

function approvalEvent(approval: PackageApproval) {
  return {
    approvalId: approval.id, packageId: approval.packageId, permissions: [...approval.permissions],
    augmentationId: approval.augmentationId, tabId: approval.tabId, origin: approval.origin,
  };
}

function validApproval(value: unknown): value is PackageApproval {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false;
  const item = value as Partial<PackageApproval>;
  return item.kind === 'package' && typeof item.id === 'string' && (item.status === 'pending' || item.status === 'approved' || item.status === 'denied')
    && typeof item.createdAt === 'string' && typeof item.expiresAt === 'string'
    && typeof item.packageId === 'string' && typeof item.packageName === 'string'
    && Array.isArray(item.permissions) && item.permissions.every((permission) => typeof permission === 'string')
    && typeof item.augmentationId === 'string' && Number.isInteger(item.tabId) && typeof item.origin === 'string';
}

function randomID() {
  const values = crypto.getRandomValues(new Uint32Array(4));
  return [...values].map((value) => value.toString(16).padStart(8, '0')).join('');
}

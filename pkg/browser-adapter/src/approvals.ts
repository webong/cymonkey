import type {StorageArea} from './runtime.js';

export type ApprovalScope = {
  kind: 'package' | 'userscript';
  subjectId: string;
  augmentationId?: string;
  revision?: string;
  targetId?: string;
  origin?: string;
  permissions: string[];
};

export type ApprovalRecord = ApprovalScope & {
  id: string;
  status: 'pending' | 'approved' | 'denied';
  createdAt: string;
  expiresAt: string;
};

export type ApprovalDependencies = {
  storage: StorageArea;
  now?: () => number;
  newId?: () => string;
  lifetimeMs?: number;
  storageKey?: string;
  onChange?: (event: {type: 'requested' | 'approved' | 'denied' | 'consumed'; approval: ApprovalRecord}) => Promise<void> | void;
};

const identifier = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;

export function createApprovalManager(dependencies: ApprovalDependencies) {
  if (!dependencies.storage?.get || !dependencies.storage?.set) throw new Error('approval storage is required');
  const now = dependencies.now ?? Date.now;
  const newId = dependencies.newId ?? (() => crypto.randomUUID());
  const lifetime = dependencies.lifetimeMs ?? 5 * 60 * 1000;
  const key = dependencies.storageKey ?? 'cymonkey.browserAdapter.approvals.v1';
  if (!Number.isInteger(lifetime) || lifetime < 1 || lifetime > 60 * 60 * 1000) throw new Error('approval lifetime is invalid');
  let queue = Promise.resolve<unknown>(undefined);
  const serialize = <T>(operation: () => Promise<T>): Promise<T> => {
    const result = queue.then(operation, operation);
    queue = result.catch(() => undefined);
    return result;
  };

  async function read(): Promise<ApprovalRecord[]> {
    const stored = await dependencies.storage.get(key);
    const value = stored[key];
    const records = Array.isArray(value) ? value.filter(validRecord).map((record) => ({
      ...validateScope(record), id: record.id, status: record.status,
      createdAt: record.createdAt, expiresAt: record.expiresAt,
    })) : [];
    return records.filter((record) => Date.parse(record.expiresAt) > now());
  }
  async function write(records: ApprovalRecord[]) {
    await dependencies.storage.set({[key]: records});
  }
  async function changed(type: 'requested' | 'approved' | 'denied' | 'consumed', approval: ApprovalRecord) {
    await dependencies.onChange?.({type, approval: structuredClone(approval)});
  }

  return {
    list: () => serialize(async () => (await read()).filter((record) => record.status === 'pending').map((record) => structuredClone(record))),
    request: (input: ApprovalScope, approvalId?: string) => serialize(async () => {
      const scope = validateScope(input);
      const records = await read();
      if (approvalId !== undefined) {
        if (!identifier.test(approvalId)) throw new Error('approval id is invalid');
        const index = records.findIndex((record) => record.id === approvalId);
        const record = records[index];
        if (!record || !sameScope(record, scope)) throw new Error('approval does not match this request');
        if (record.status === 'denied') throw new Error('approval was denied');
        if (record.status === 'pending') return {approved: false as const, approval: structuredClone(record)};
        records.splice(index, 1);
        await write(records);
        await changed('consumed', record);
        return {approved: true as const};
      }
      const pending = records.find((record) => record.status === 'pending' && sameScope(record, scope));
      if (pending) return {approved: false as const, approval: structuredClone(pending)};
      const created = now();
      const id = newId();
      if (!identifier.test(id) || records.some((record) => record.id === id)) throw new Error('approval id is invalid or repeated');
      const approval: ApprovalRecord = {
        ...scope, id, status: 'pending', createdAt: new Date(created).toISOString(), expiresAt: new Date(created + lifetime).toISOString(),
      };
      records.push(approval);
      await write(records);
      await changed('requested', approval);
      return {approved: false as const, approval: structuredClone(approval)};
    }),
    resolve: (id: string, decision: 'approve' | 'deny') => serialize(async () => {
      if (!identifier.test(id) || (decision !== 'approve' && decision !== 'deny')) throw new Error('approval decision is invalid');
      const records = await read();
      const record = records.find((item) => item.id === id);
      if (!record || record.status !== 'pending') throw new Error('approval is no longer pending');
      record.status = decision === 'approve' ? 'approved' : 'denied';
      await write(records);
      await changed(record.status, record);
      return structuredClone(record);
    }),
  };
}

function validateScope(value: ApprovalScope): ApprovalScope {
  if (!value || (value.kind !== 'package' && value.kind !== 'userscript') || typeof value.subjectId !== 'string' || !identifier.test(value.subjectId)
    || (value.revision !== undefined && (typeof value.revision !== 'string' || value.revision.length > 256))
    || (value.augmentationId !== undefined && (typeof value.augmentationId !== 'string' || !identifier.test(value.augmentationId)))
    || (value.targetId !== undefined && (typeof value.targetId !== 'string' || value.targetId.length > 256))
    || (value.origin !== undefined && (typeof value.origin !== 'string' || value.origin.length > 512))
    || !Array.isArray(value.permissions) || value.permissions.length > 128
    || value.permissions.some((permission) => typeof permission !== 'string' || permission.length > 256)) {
    throw new Error('approval scope is invalid');
  }
  return {
    kind: value.kind, subjectId: value.subjectId,
    ...(value.augmentationId === undefined ? {} : {augmentationId: value.augmentationId}),
    ...(value.revision === undefined ? {} : {revision: value.revision}),
    ...(value.targetId === undefined ? {} : {targetId: value.targetId}),
    ...(value.origin === undefined ? {} : {origin: value.origin}),
    permissions: [...new Set(value.permissions)].sort(),
  };
}

function sameScope(left: ApprovalScope, right: ApprovalScope) {
  return left.kind === right.kind && left.subjectId === right.subjectId && left.augmentationId === right.augmentationId && left.revision === right.revision
    && left.targetId === right.targetId && left.origin === right.origin
    && JSON.stringify([...left.permissions].sort()) === JSON.stringify(right.permissions);
}

function validRecord(value: unknown): value is ApprovalRecord {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const record = value as Partial<ApprovalRecord>;
  try {
    validateScope(record as ApprovalScope);
    return typeof record.id === 'string' && identifier.test(record.id)
      && (record.status === 'pending' || record.status === 'approved' || record.status === 'denied')
      && typeof record.createdAt === 'string' && typeof record.expiresAt === 'string';
  } catch { return false; }
}

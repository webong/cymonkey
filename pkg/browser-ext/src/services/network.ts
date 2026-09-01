import { isRecord } from '../types';
import { publishCymonkeyEvent } from './events';
import { readPlatformRecord, writePlatformRecord } from './storage';

const ownershipKey = 'cymonkey.network.ruleOwnership';

export async function installOwnedRules(owner: string, input: Record<string, unknown>) {
  if (!Array.isArray(input.rules) || input.rules.length === 0) throw new Error('rules must be a non-empty array');
  const rules = input.rules.map((value) => {
    if (!isRecord(value) || !Number.isInteger(value.id) || Number(value.id) <= 0) {
      throw new Error('network rule id must be a positive integer');
    }
    return value;
  });
  const ownership = await readPlatformRecord(ownershipKey);
  for (const rule of rules) {
    const id = Number(rule.id);
    if (ownership[String(id)] && ownership[String(id)] !== owner) {
      throw new Error(`network rule ${id} belongs to ${JSON.stringify(ownership[String(id)])}`);
    }
  }
  const ruleIds = rules.map((rule) => Number(rule.id));
  await browser.declarativeNetRequest.updateDynamicRules({
    removeRuleIds: ruleIds,
    addRules: rules as unknown as Parameters<typeof browser.declarativeNetRequest.updateDynamicRules>[0]['addRules'],
  });
  for (const id of ruleIds) ownership[String(id)] = owner;
  await writePlatformRecord(ownershipKey, ownership);
  await publishCymonkeyEvent('network.rules.installed', { augmentationId: owner, ruleIds });
  return { ok: true, ruleIds };
}

export async function removeOwnedRules(owner: string, input: Record<string, unknown>) {
  if (!Array.isArray(input.ruleIds) || input.ruleIds.length === 0 || input.ruleIds.some((id) => !Number.isInteger(id))) {
    throw new Error('ruleIds must be a non-empty array of integers');
  }
  const ruleIds = input.ruleIds as number[];
  const ownership = await readPlatformRecord(ownershipKey);
  for (const id of ruleIds) {
    if (ownership[String(id)] !== owner) throw new Error(`network rule ${id} is not owned by ${JSON.stringify(owner)}`);
  }
  await browser.declarativeNetRequest.updateDynamicRules({ removeRuleIds: ruleIds });
  for (const id of ruleIds) delete ownership[String(id)];
  await writePlatformRecord(ownershipKey, ownership);
  await publishCymonkeyEvent('network.rules.removed', { augmentationId: owner, ruleIds });
  return { ok: true, ruleIds };
}

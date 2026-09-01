export const CYMONKEY_PROTOCOL_VERSION = 'cymonkey/v1alpha1';
export const CYMONKEY_RUNTIME_SYMBOL = Symbol.for('cymonkey.jangolova.runtime');
export const CYMONKEY_DOMAIN = 'render';
export const CYMONKEY_RUNTIME = 'threejs';
export const CYMONKEY_DRIVER = 'in-page-runtime';


export type ResourceKind = 'scene' | 'object' | 'ui' | 'camera' | 'material' | 'animation' | 'timeline' | 'artifact' | 'event';
export type CymonkeyRequest = { id?: unknown; method: string; params?: Record<string, unknown> };
export type Registration = {
  id: string;
  kind: ResourceKind;
  label?: string;
  target: unknown;
  actions: string[];
  describe?: (target: unknown) => Record<string, unknown>;
};

type CymonkeyEvent = { id: string; type: string; domain: typeof CYMONKEY_DOMAIN; runtime: typeof CYMONKEY_RUNTIME; driver: typeof CYMONKEY_DRIVER; sourceId?: string; occurredAt: string; data?: unknown };

const capabilities = [
  capability('resource.describe', 'read', ['scene', 'object', 'ui', 'camera', 'material', 'animation', 'timeline', 'artifact']),
  capability('object.visibility.set', 'write', ['object']),
  capability('object.transform.set', 'write', ['object', 'camera']),
  capability('camera.projection.set', 'write', ['camera']),
  capability('material.property.set', 'write', ['material']),
  capability('animation.play', 'write', ['animation']),
  capability('animation.stop', 'write', ['animation']),
];

function capability(name: string, effect: 'read' | 'write', resourceKinds: ResourceKind[]) {
  return {
    name,
    domain: CYMONKEY_DOMAIN,
    runtime: CYMONKEY_RUNTIME,
    driver: CYMONKEY_DRIVER,
    support: 'native',
    lifetime: 'attachment',
    persistence: 'session',
    effect,
    resourceKinds,
    inputSchema: { type: 'object', additionalProperties: true },
  };
}

export class ThreeJSCymonkey {
  readonly protocolVersion = CYMONKEY_PROTOCOL_VERSION;
  #registrations = new Map<string, Registration>();
  #events: CymonkeyEvent[] = [];
  #revision = 0;
  #eventSequence = 0;

  register(registration: Registration) {
    validateRegistration(registration);
    if (this.#registrations.has(registration.id)) throw new Error(`duplicate Cymonkey resource ${JSON.stringify(registration.id)}`);
    this.#registrations.set(registration.id, { ...registration, actions: [...new Set(registration.actions)].sort() });
    this.#revision += 1;
    this.publish('resource.registered', registration.id, { kind: registration.kind });
    return () => this.unregister(registration.id);
  }

  unregister(id: string) {
    if (!this.#registrations.delete(id)) throw new Error(`Cymonkey resource ${JSON.stringify(id)} is not registered`);
    this.#revision += 1;
    this.publish('resource.unregistered', id);
  }

  installGlobal(target: Record<PropertyKey, unknown> = globalThis as Record<PropertyKey, unknown>) {
    if (target[CYMONKEY_RUNTIME_SYMBOL] && target[CYMONKEY_RUNTIME_SYMBOL] !== this) {
      throw new Error('another Cymonkey runtime is already installed');
    }
    Object.defineProperty(target, CYMONKEY_RUNTIME_SYMBOL, { configurable: true, value: this });
    return () => { if (target[CYMONKEY_RUNTIME_SYMBOL] === this) delete target[CYMONKEY_RUNTIME_SYMBOL]; };
  }

  async dispatch(request: CymonkeyRequest) {
    try {
      const params = isRecord(request.params) ? request.params : {};
      let result: unknown;
      if (request.method === 'hello') result = this.hello();
      else if (request.method === 'capabilities') result = capabilities;
      else if (request.method === 'describe') result = this.describe();
      else if (request.method === 'act') result = await this.act(params);
      else if (request.method === 'events') result = this.events(params);
      else if (request.method === 'health') result = { status: 'ready', observedAt: new Date().toISOString() };
      else throw cymonkeyError('method_not_found', `unsupported Cymonkey method ${JSON.stringify(request.method)}`);
      return { id: request.id ?? null, result };
    } catch (error) {
      const value = error instanceof CymonkeyRuntimeError ? error : cymonkeyError('internal_error', error instanceof Error ? error.message : String(error));
      return { id: request.id ?? null, error: { code: value.code, message: value.message } };
    }
  }

  hello() {
    return {
      protocolVersion: CYMONKEY_PROTOCOL_VERSION,
      implementation: { name: 'cymonkey-threejs', version: '0.2.0' },
      domains: [CYMONKEY_DOMAIN],
      runtimes: [CYMONKEY_RUNTIME],
      drivers: [CYMONKEY_DRIVER],
      features: ['explicit-registration', 'stable-ids', 'events.cursor'],
    };
  }

  describe() {
    return {
      revision: String(this.#revision),
      surfaces: [...this.#registrations.values()].map((entry) => ({
        id: entry.id,
        domain: CYMONKEY_DOMAIN,
        runtime: CYMONKEY_RUNTIME,
        kind: entry.kind,
        label: entry.label,
        properties: entry.describe ? entry.describe(entry.target) : describeTarget(entry.kind, entry.target),
        actions: entry.actions,
      })).sort((left, right) => left.id.localeCompare(right.id)),
      augmentations: [],
    };
  }

  async act(params: Record<string, unknown>) {
    const name = requireString(params.name, 'action name');
    const input = isRecord(params.input) ? params.input : {};
    const targetId = typeof input.targetId === 'string' && input.targetId
      ? input.targetId
      : requireString(params.targetId, 'input.targetId');
    const registration = this.#registrations.get(targetId);
    if (!registration) throw cymonkeyError('target_not_allowlisted', 'Cymonkey target is not registered');
    if (!registration.actions.includes(name)) throw cymonkeyError('action_not_allowlisted', 'Cymonkey action is not allowed for this target');
    if (typeof input.expectedRevision === 'string' && input.expectedRevision !== '' && input.expectedRevision !== String(this.#revision)) {
      throw cymonkeyError('stale_revision', `expected revision ${input.expectedRevision} is stale; current revision is ${this.#revision}`);
    }
    const result = applyAction(name, registration.target, input);
    this.#revision += 1;
    this.publish('resource.changed', targetId, { action: name });
    return result;
  }

  events(params: Record<string, unknown> = {}) {
    const after = Number.parseInt(String(params.after || '0'), 10);
    if (!Number.isSafeInteger(after) || after < 0) throw cymonkeyError('invalid_input', 'events.after must be a non-negative cursor');
    const types = new Set(Array.isArray(params.types) ? params.types.filter((value): value is string => typeof value === 'string') : []);
    const limit = Math.min(Math.max(Number(params.limit) || 100, 1), 256);
    return {
      events: this.#events.filter((event) => Number(event.id) > after && (!types.size || types.has(event.type))).slice(0, limit),
      cursor: String(this.#eventSequence),
    };
  }

  publish(type: string, sourceId?: string, data?: unknown) {
    this.#eventSequence += 1;
    const eventType = type.startsWith('event:') ? type : `event:${type}`;
    this.#events.push({ id: String(this.#eventSequence), type: eventType, domain: CYMONKEY_DOMAIN, runtime: CYMONKEY_RUNTIME, driver: CYMONKEY_DRIVER, sourceId, occurredAt: new Date().toISOString(), data });
    if (this.#events.length > 256) this.#events.splice(0, this.#events.length - 256);
  }
}


function applyAction(name: string, target: unknown, input: Record<string, unknown>) {
  const value = target as Record<string, any>;
  if (!value || (typeof value !== 'object' && typeof value !== 'function')) throw cymonkeyError('target_unavailable', 'registered target is unavailable');
  if (name === 'resource.describe') return describeTarget('object', target);
  if (name === 'object.visibility.set') { value.visible = requireBoolean(input.visible, 'visible'); return { ok: true }; }
  if (name === 'object.transform.set') {
    applyVector(value.position, input.position);
    applyVector(value.rotation, input.rotation);
    applyVector(value.scale, input.scale);
    value.updateMatrix?.(); value.updateMatrixWorld?.(true);
    return { ok: true };
  }
  if (name === 'camera.projection.set') {
    for (const key of ['fov', 'near', 'far', 'zoom', 'left', 'right', 'top', 'bottom']) {
      if (input[key] !== undefined) value[key] = requireNumber(input[key], key);
    }
    value.updateProjectionMatrix?.();
    return { ok: true };
  }
  if (name === 'material.property.set') {
    const property = requireString(input.property, 'property');
    if (property === '__proto__' || property === 'constructor' || !(property in value)) throw cymonkeyError('invalid_input', 'material property is not writable');
    value[property] = input.value; value.needsUpdate = true;
    return { ok: true };
  }
  if (name === 'animation.play') { value.reset?.(); value.play?.(); return { ok: true }; }
  if (name === 'animation.stop') { value.stop?.(); return { ok: true }; }
  throw cymonkeyError('unsupported_action', `unsupported Three.js Cymonkey action ${JSON.stringify(name)}`);
}

function applyVector(target: any, value: unknown) {
  if (value === undefined) return;
  if (!target || !isRecord(value)) throw cymonkeyError('invalid_input', 'transform component is invalid');
  const x = value.x === undefined ? target.x : requireNumber(value.x, 'x');
  const y = value.y === undefined ? target.y : requireNumber(value.y, 'y');
  const z = value.z === undefined ? target.z : requireNumber(value.z, 'z');
  if (typeof target.set === 'function') target.set(x, y, z); else Object.assign(target, { x, y, z });
}

function describeTarget(kind: ResourceKind, target: unknown) {
  const value = target as Record<string, any> | null;
  return {
    kind,
    name: typeof value?.name === 'string' ? value.name : undefined,
    visible: typeof value?.visible === 'boolean' ? value.visible : undefined,
    position: vector(value?.position),
    rotation: vector(value?.rotation),
    scale: vector(value?.scale),
  };
}

function vector(value: any) {
  return value && ['x', 'y', 'z'].every((key) => typeof value[key] === 'number')
    ? { x: value.x, y: value.y, z: value.z }
    : undefined;
}

function validateRegistration(value: Registration) {
  if (!value || !value.target) throw new Error('Cymonkey registration requires a target');
  if (!value.id.match(/^[a-z][a-z0-9-]{0,31}:[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$/)) throw new Error('invalid stable Cymonkey resource ID');
  if (!value.id.startsWith(`${value.kind}:`)) throw new Error('Cymonkey resource ID prefix must match kind');
  if (!Array.isArray(value.actions) || value.actions.some((action) => typeof action !== 'string')) throw new Error('Cymonkey actions must be an array');
}

class CymonkeyRuntimeError extends Error { constructor(readonly code: string, message: string) { super(message); } }
function cymonkeyError(code: string, message: string) { return new CymonkeyRuntimeError(code, message); }
function requireString(value: unknown, name: string) { if (typeof value !== 'string' || !value) throw cymonkeyError('invalid_input', `${name} is required`); return value; }
function requireNumber(value: unknown, name: string) { if (typeof value !== 'number' || !Number.isFinite(value)) throw cymonkeyError('invalid_input', `${name} must be finite`); return value; }
function requireBoolean(value: unknown, name: string) { if (typeof value !== 'boolean' || value === null) throw cymonkeyError('invalid_input', `${name} must be boolean`); return value; }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }

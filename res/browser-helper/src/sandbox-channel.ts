import {connectSandboxHost as connectExtSandboxHost, startSandboxGuest as startExtSandboxGuest} from 'ext-web/bridge';

export type SandboxMessageEvent = {source: unknown; data: unknown; ports?: MessagePort[]};
export type SandboxWindow = {
  addEventListener(type: 'message', listener: (event: SandboxMessageEvent) => void): void;
  removeEventListener(type: 'message', listener: (event: SandboxMessageEvent) => void): void;
  postMessage(message: unknown, targetOrigin: string, transfer?: Transferable[]): void;
};

const identifier = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const valid = (value: unknown): value is string => typeof value === 'string' && identifier.test(value);
const record = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

// The caller creates the iframe and supplies a cryptographically random nonce.
// Ext binds the MessagePort to the expected source and nonce; Cymonkey binds
// its augmentation and package identity inside the transferred context.
export function connectSandboxHost(dependencies: {
  host: SandboxWindow;
  guest: SandboxWindow;
  id: string;
  nonce: string;
  context: {augmentationId: string; configuration: Record<string, unknown>};
  createChannel: () => MessageChannel;
  timeoutMs?: number;
}): Promise<MessagePort> {
  if (!dependencies || !valid(dependencies.id) || !valid(dependencies.context?.augmentationId)
    || !record(dependencies.context.configuration)) throw new Error('sandbox host dependencies are invalid');
  return connectExtSandboxHost({
    host: dependencies.host,
    guest: dependencies.guest,
    nonce: dependencies.nonce,
    context: {id: dependencies.id, ...dependencies.context},
    createChannel: dependencies.createChannel,
    timeoutMs: dependencies.timeoutMs,
  });
}

// Run in the sandbox page after the consuming extension chose and loaded its
// package. A mismatched package/augmentation identity never reaches connect.
export function startSandboxGuest(dependencies: {
  guest: SandboxWindow;
  parent: SandboxWindow;
  id: string;
  nonce: string;
  load: () => void | Promise<void>;
  connect: (context: {augmentationId: string; configuration: Record<string, unknown>}, port: MessagePort) => void | Promise<void>;
}) {
  if (!dependencies || !valid(dependencies.id) || typeof dependencies.load !== 'function'
    || typeof dependencies.connect !== 'function') throw new Error('sandbox guest dependencies are invalid');
  return startExtSandboxGuest({
    self: dependencies.guest,
    parent: dependencies.parent,
    nonce: dependencies.nonce,
    load: dependencies.load,
    connect(context, port) {
      if (!record(context) || context.id !== dependencies.id || !valid(context.augmentationId)
        || !record(context.configuration)) throw new Error('sandbox package context is invalid');
      return dependencies.connect({augmentationId: context.augmentationId, configuration: context.configuration}, port);
    },
  });
}

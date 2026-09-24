export type SandboxMessageEvent = {source: unknown; data: unknown; ports?: MessagePort[]};
export type SandboxWindow = {
  addEventListener(type: 'message', listener: (event: SandboxMessageEvent) => void): void;
  removeEventListener(type: 'message', listener: (event: SandboxMessageEvent) => void): void;
  postMessage(message: unknown, targetOrigin: string, transfer?: Transferable[]): void;
};

const record = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);
const identifier = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const valid = (value: unknown) => typeof value === 'string' && identifier.test(value);
const errorMessage = (value: unknown) => value instanceof Error ? value.message : String(value);

// The caller owns iframe creation and must supply a cryptographically random
// nonce. Source and nonce are both checked because sandbox pages can have an
// opaque origin and therefore require postMessage's "*" target origin.
export function connectSandboxHost(dependencies: {
  host: SandboxWindow;
  guest: SandboxWindow;
  id: string;
  nonce: string;
  context: {augmentationId: string; configuration: Record<string, unknown>};
  createChannel: () => MessageChannel;
  timeoutMs?: number;
}): Promise<MessagePort> {
  if (!dependencies?.host?.addEventListener || !dependencies.guest?.postMessage
    || !valid(dependencies.id) || !valid(dependencies.nonce)
    || !valid(dependencies.context?.augmentationId) || !record(dependencies.context.configuration)
    || typeof dependencies.createChannel !== 'function') throw new Error('sandbox host dependencies are invalid');
  const timeoutMs = dependencies.timeoutMs ?? 10_000;
  if (!Number.isInteger(timeoutMs) || timeoutMs < 1) throw new Error('sandbox connection timeout is invalid');
  return new Promise((resolve, reject) => {
    let port: MessagePort | undefined;
    let connected = false;
    const cleanup = () => { clearTimeout(timer); dependencies.host.removeEventListener('message', receive); };
    const fail = (message: string) => { cleanup(); port?.close(); reject(new Error(message)); };
    const receive = (event: SandboxMessageEvent) => {
      if (event.source !== dependencies.guest || !record(event.data)
        || event.data.id !== dependencies.id || event.data.nonce !== dependencies.nonce) return;
      if (event.data.channel === 'cymonkey.jangolova.sandbox.ready' && !port) {
        if (event.data.status !== 'ready') return fail(typeof event.data.error === 'string' ? event.data.error : 'sandbox package failed to load');
        try {
          const channel = dependencies.createChannel();
          port = channel.port1;
          dependencies.guest.postMessage({
            channel: 'cymonkey.jangolova.sandbox.connect', nonce: dependencies.nonce,
            context: dependencies.context,
          }, '*', [channel.port2]);
        } catch (error) { fail(errorMessage(error)); }
      } else if (event.data.channel === 'cymonkey.jangolova.sandbox.connected' && port && !connected) {
        connected = true;
        if (event.data.status !== 'connected') return fail(typeof event.data.error === 'string' ? event.data.error : 'sandbox package failed to connect');
        cleanup();
        resolve(port);
      }
    };
    const timer = setTimeout(() => fail('sandbox package did not become ready'), timeoutMs);
    dependencies.host.addEventListener('message', receive);
  });
}

// Run in the sandbox page after the consuming extension has chosen and loaded
// its package. The connector knows only the package's connect callback.
export function startSandboxGuest(dependencies: {
  guest: SandboxWindow;
  parent: SandboxWindow;
  id: string;
  nonce: string;
  load: () => void | Promise<void>;
  connect: (context: {augmentationId: string; configuration: Record<string, unknown>}, port: MessagePort) => void | Promise<void>;
}) {
  if (!dependencies?.guest?.addEventListener || !dependencies.parent?.postMessage
    || !valid(dependencies.id) || !valid(dependencies.nonce)
    || typeof dependencies.load !== 'function' || typeof dependencies.connect !== 'function') {
    throw new Error('sandbox guest dependencies are invalid');
  }
  let disposed = false;
  let accepted = false;
  const report = (channel: string, status: string, error?: string) => {
    if (!disposed) dependencies.parent.postMessage({channel, id: dependencies.id, nonce: dependencies.nonce, status, ...(error ? {error} : {})}, '*');
  };
  const receive = (event: SandboxMessageEvent) => {
    if (disposed || accepted || event.source !== dependencies.parent || !record(event.data)
      || event.data.channel !== 'cymonkey.jangolova.sandbox.connect' || event.data.nonce !== dependencies.nonce
      || event.ports?.length !== 1 || !record(event.data.context)
      || !valid(event.data.context.augmentationId) || !record(event.data.context.configuration)) return;
    accepted = true;
    const [port] = event.ports;
    const context = {augmentationId: event.data.context.augmentationId as string, configuration: event.data.context.configuration};
    void Promise.resolve().then(() => dependencies.connect(context, port)).then(
      () => report('cymonkey.jangolova.sandbox.connected', 'connected'),
      (error) => report('cymonkey.jangolova.sandbox.connected', 'failed', errorMessage(error)),
    );
  };
  dependencies.guest.addEventListener('message', receive);
  void Promise.resolve().then(dependencies.load).then(
    () => report('cymonkey.jangolova.sandbox.ready', 'ready'),
    (error) => report('cymonkey.jangolova.sandbox.ready', 'failed', errorMessage(error)),
  );
  return () => { disposed = true; dependencies.guest.removeEventListener('message', receive); };
}

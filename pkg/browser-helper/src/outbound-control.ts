const configurationKey = 'cymonkey.outboundControl.v1';

type ControlSource = 'xallet-spook' | 'authenticated-websocket' | 'extension-origin';

export type OutboundControlStatus = 'disabled' | 'connecting' | 'authenticating' | 'connected' | 'unavailable';

export type OutboundControlConfiguration = {
  endpoint: string;
  token: string;
  expiresAt: string;
};

type StorageArea = {
  get(key: string): Promise<Record<string, unknown>>;
  set(value: Record<string, unknown>): Promise<void>;
  remove(key: string): Promise<void>;
};

type SocketLike = {
  readyState: number;
  send(data: string): void;
  close(code?: number, reason?: string): void;
  addEventListener(type: 'open' | 'message' | 'close' | 'error', listener: (event: Event | MessageEvent) => void): void;
};

type SocketFactory = (endpoint: string) => SocketLike;
type ControlHandler = (message: Record<string, unknown>, source: ControlSource) => Promise<Record<string, unknown>>;

export class OutboundControlClient {
  private socket: SocketLike | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  private heartbeatTimer: ReturnType<typeof setInterval> | undefined;
  private retry = 0;
  private status: OutboundControlStatus = 'disabled';
  private configuration: OutboundControlConfiguration | null = null;

  constructor(
    private readonly storage: StorageArea,
    private readonly handleControl: ControlHandler,
    private readonly onStatus: (status: OutboundControlStatus) => void = () => undefined,
    private readonly createSocket: SocketFactory = (endpoint) => new WebSocket(endpoint),
  ) {}

  async start() {
    const stored = await this.storage.get(configurationKey);
    const value = stored[configurationKey];
    if (value === undefined) return this.setStatus('disabled');
    try {
      this.configuration = validateOutboundConfiguration(value);
      this.connect();
    } catch {
      await this.storage.remove(configurationKey);
      this.configuration = null;
      this.setStatus('unavailable');
    }
  }

  async configure(value: unknown) {
    const configuration = validateOutboundConfiguration(value);
    await this.storage.set({[configurationKey]: configuration});
    this.configuration = configuration;
    this.retry = 0;
    this.disconnectSocket();
    this.connect();
    return this.describe();
  }

  async disable() {
    await this.storage.remove(configurationKey);
    this.configuration = null;
    this.retry = 0;
    this.disconnectSocket();
    this.setStatus('disabled');
    return this.describe();
  }

  describe() {
    return {
      status: this.status,
      configured: Boolean(this.configuration),
      endpoint: this.configuration?.endpoint ?? null,
      expiresAt: this.configuration?.expiresAt ?? null,
    };
  }

  stop() {
    this.configuration = null;
    this.disconnectSocket();
    this.setStatus('disabled');
  }

  private connect() {
    const configuration = this.configuration;
    if (!configuration || Date.parse(configuration.expiresAt) <= Date.now()) {
      this.setStatus(configuration ? 'unavailable' : 'disabled');
      return;
    }
    this.setStatus('connecting');
    const socket = this.createSocket(configuration.endpoint);
    this.socket = socket;
    socket.addEventListener('open', () => {
      if (this.socket !== socket || !this.configuration) return;
      this.setStatus('authenticating');
      socket.send(JSON.stringify({
        type: 'CYMONKEY_EXTENSION_AUTH',
        protocolVersion: 'cymonkey.browser-extension/v1alpha1',
        token: this.configuration.token,
      }));
    });
    socket.addEventListener('message', (event) => void this.receive(socket, (event as MessageEvent).data));
    socket.addEventListener('close', () => this.reconnect(socket));
    socket.addEventListener('error', () => this.reconnect(socket));
  }

  private async receive(socket: SocketLike, raw: unknown) {
    if (this.socket !== socket || typeof raw !== 'string' || raw.length > 4 * 1024 * 1024) return;
    let value: unknown;
    try { value = JSON.parse(raw); } catch { return; }
    if (!isRecord(value)) return;
    if (value.type === 'CYMONKEY_EXTENSION_AUTHENTICATED') {
      this.retry = 0;
      this.setStatus('connected');
      this.startHeartbeat(socket);
      return;
    }
    if (this.status !== 'connected' || !isExtensionControlCall(value)) return;
    const id = typeof value.id === 'string' || typeof value.id === 'number' ? value.id : null;
    try {
      const response = await this.handleControl(value, 'authenticated-websocket');
      socket.send(JSON.stringify({type: 'CYMONKEY_EXTENSION_RESPONSE', id, ...response}));
    } catch (error) {
      socket.send(JSON.stringify({type: 'CYMONKEY_EXTENSION_RESPONSE', id, ok: false, error: errorMessage(error)}));
    }
  }

  private startHeartbeat(socket: SocketLike) {
    if (this.heartbeatTimer) clearInterval(this.heartbeatTimer);
    this.heartbeatTimer = setInterval(() => {
      if (this.socket === socket && socket.readyState === 1) {
        socket.send(JSON.stringify({type: 'CYMONKEY_EXTENSION_PING', occurredAt: new Date().toISOString()}));
      }
    }, 20_000);
  }

  private reconnect(socket: SocketLike) {
    if (this.socket !== socket) return;
    this.socket = null;
    if (this.heartbeatTimer) clearInterval(this.heartbeatTimer);
    this.heartbeatTimer = undefined;
    if (!this.configuration || Date.parse(this.configuration.expiresAt) <= Date.now()) {
      this.setStatus('unavailable');
      return;
    }
    this.setStatus('unavailable');
    const delays = [1_000, 2_000, 5_000, 10_000, 30_000];
    const delay = delays[Math.min(this.retry, delays.length - 1)]!;
    this.retry += 1;
    this.reconnectTimer = setTimeout(() => this.connect(), delay);
  }

  private disconnectSocket() {
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    if (this.heartbeatTimer) clearInterval(this.heartbeatTimer);
    this.reconnectTimer = undefined;
    this.heartbeatTimer = undefined;
    const socket = this.socket;
    this.socket = null;
    socket?.close(1000, 'Cymonkey control reconfigured');
  }

  private setStatus(status: OutboundControlStatus) {
    this.status = status;
    this.onStatus(status);
  }
}

export function validateOutboundConfiguration(value: unknown): OutboundControlConfiguration {
  if (!isRecord(value) || typeof value.endpoint !== 'string' || typeof value.token !== 'string' || typeof value.expiresAt !== 'string') {
    throw new Error('outbound control configuration requires endpoint, token, and expiresAt');
  }
  let endpoint: URL;
  try { endpoint = new URL(value.endpoint); } catch { throw new Error('outbound control endpoint is invalid'); }
  if (endpoint.username || endpoint.password || (endpoint.protocol !== 'wss:' && endpoint.protocol !== 'ws:')) {
    throw new Error('outbound control endpoint must be ws or wss without URL credentials');
  }
  const loopback = endpoint.hostname === 'localhost' || endpoint.hostname === '127.0.0.1' || endpoint.hostname === '[::1]';
  if (endpoint.protocol === 'ws:' && !loopback) throw new Error('plaintext outbound control must use loopback');
  if (value.token.length < 16 || value.token.length > 4096) throw new Error('outbound control token has invalid length');
  const expiresAt = Date.parse(value.expiresAt);
  if (!Number.isFinite(expiresAt) || expiresAt <= Date.now() || expiresAt > Date.now() + 24 * 60 * 60 * 1000) {
    throw new Error('outbound control token must expire within 24 hours');
  }
  return {endpoint: endpoint.toString(), token: value.token, expiresAt: new Date(expiresAt).toISOString()};
}

function isExtensionControlCall(message: unknown): message is Record<string, unknown> {
  return isRecord(message) && message.type === 'CYMONKEY_EXTENSION_CALL';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function errorMessage(value: unknown) {
  return value instanceof Error ? value.message : String(value);
}

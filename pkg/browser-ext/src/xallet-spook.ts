import type { XalletSpookState } from './types';

const hubName = 'Xallet';

export type XalletSpookBrowser = {
  management: { getAll(): Promise<Array<{ id?: string; name: string; enabled: boolean }>> };
  runtime: { sendMessage(extensionId: string, message: unknown): Promise<unknown> };
};

export class XalletSpookClient {
  private hubId: string | null = null;
  private timer: ReturnType<typeof setInterval> | undefined;

  constructor(
    private readonly name: string,
    private state: XalletSpookState,
    private readonly uiPath: string,
    private readonly onStatus: (status: XalletSpookState['xalletSpook']) => void = () => undefined,
    private readonly api: XalletSpookBrowser,
  ) {}

  start() {
    void this.probe();
    this.timer = setInterval(() => void this.probe(), 10_000);
  }

  stop() {
    if (this.timer) clearInterval(this.timer);
    this.timer = undefined;
  }

  acceptsExternalSender(senderId: string | undefined) {
    return Boolean(senderId && this.hubId && senderId === this.hubId);
  }

  async updateState(state: XalletSpookState) {
    this.state = state;
    if (!this.hubId) return;
    try {
      await this.api.runtime.sendMessage(this.hubId, {
        type: 'UPDATE_SPOKE_STATE',
        payload: state,
      });
    } catch {
      this.hubId = null;
      this.setStatus('unavailable');
    }
  }

  async probe() {
    try {
      const extensions = await this.api.management.getAll();
      const hub = extensions.find((extension) => extension.name === hubName && extension.enabled);
      if (!hub?.id) {
        this.hubId = null;
        this.setStatus('unavailable');
        return;
      }
      const connectedState = { ...this.state, xalletSpook: 'connected' as const };
      await this.api.runtime.sendMessage(hub.id, {
        type: 'REGISTER_SPOKE',
        payload: {
          name: this.name,
          initialState: connectedState,
          uiPath: this.uiPath,
        },
      });
      this.hubId = hub.id;
      this.setStatus('connected');
    } catch {
      this.hubId = null;
      this.setStatus('unavailable');
    }
  }

  private setStatus(status: XalletSpookState['xalletSpook']) {
    this.state = { ...this.state, xalletSpook: status };
    this.onStatus(status);
  }
}

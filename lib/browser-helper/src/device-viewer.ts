/** Session-scoped WebRTC viewer. Host supplies a credential from POST
 * /v1/device-sessions/{id}/viewer, never its operator token. */

export type PointerButton = 'left' | 'middle' | 'right';

export interface DeviceViewerOptions {
  /** Same-origin `/v1/device-viewer/{id}` path returned by the host. */
  url: string;
  /** Viewer credential for that one session. */
  token: string;
  video: HTMLVideoElement;
  iceServers?: RTCIceServer[];
  relayOnly?: boolean;
}

export interface DeviceViewerConnectOptions {
  microphone?: boolean;
  keyboard?: boolean;
  pointer?: boolean;
}

export type DeviceViewerSignal =
  | {type: 'offer' | 'answer'; sdp?: string}
  | {type: 'ice-candidate'; candidate: RTCIceCandidateInit}
  | {type: 'bye'};

export interface DeviceViewerRoute {
  localType: string;
  remoteType: string;
  protocol?: string;
  state?: string;
  bytesSent?: number;
  bytesReceived?: number;
}

export interface DeviceViewerMedia {
  direction: 'inbound-rtp' | 'outbound-rtp';
  kind?: string;
  codec?: string;
  packetsReceived?: number;
  packetsSent?: number;
  packetsLost?: number;
  jitter?: number;
  framesDecoded?: number;
  bytesReceived?: number;
  bytesSent?: number;
}

/** Route types and counters only: never ICE credentials, addresses or SDP. */
export interface DeviceViewerDiagnostics {
  time: string;
  connectionState: RTCPeerConnectionState;
  icePolicy?: RTCIceTransportPolicy;
  routes: DeviceViewerRoute[];
  media: DeviceViewerMedia[];
}

interface HeldPointer {
  x: number;
  y: number;
  button: PointerButton;
}

type Stats = Record<string, any>;

export class DeviceViewer extends EventTarget {
  readonly url: string;
  readonly video: HTMLVideoElement;
  readonly configuration: RTCConfiguration;
  token: string;
  pc?: RTCPeerConnection;
  input?: RTCDataChannel;
  microphone?: MediaStream;
  closed = false;
  started = false;
  private readonly abort = new AbortController();
  private readonly pending: RTCIceCandidateInit[] = [];
  private serial: Promise<unknown> = Promise.resolve();
  private restarting = false;
  private heldPointer: HeldPointer | null = null;
  private holdTimer?: ReturnType<typeof setInterval>;
  private readonly releaseOnBlur: () => void;
  private readonly releaseOnHidden: () => void;

  constructor({url, token, video, iceServers = [], relayOnly = false}: DeviceViewerOptions) {
    super();
    const endpoint = new URL(url, location.href);
    if (endpoint.origin !== location.origin) throw new Error('Viewer bridge must be same-origin');
    if (!/^\/v1\/device-viewer\/[a-f0-9]+$/.test(endpoint.pathname)) throw new Error('Invalid viewer endpoint');
    this.url = endpoint.href; this.token = token; this.video = video;
    this.configuration = {iceServers, iceTransportPolicy: relayOnly ? 'relay' : 'all'};
    this.releaseOnBlur = () => this.releasePointer();
    this.releaseOnHidden = () => { if (document.hidden) this.releasePointer(); };
    window.addEventListener('blur', this.releaseOnBlur);
    document.addEventListener('visibilitychange', this.releaseOnHidden);
  }

  async action(capability: string, input: unknown = {}): Promise<any> {
    const response = await fetch(this.url, {method: 'POST', credentials: 'omit', cache: 'no-store',
      headers: {'Authorization': `Bearer ${this.token}`, 'Content-Type': 'application/json'},
      body: JSON.stringify({capability, input}), signal: this.abort.signal});
    if (!response.ok) throw new Error(`Device session: HTTP ${response.status}`);
    return (await response.json()).output;
  }

  signal(value: DeviceViewerSignal): Promise<unknown> {
    const task = this.serial.then(() => this.action('webrtc.signal', value));
    this.serial = task.catch(() => {}); return task;
  }

  private state(value: string): void { this.dispatchEvent(new CustomEvent('state', {detail: value})); }

  async connect({microphone = false, keyboard = false, pointer = false}: DeviceViewerConnectOptions = {}): Promise<void> {
    if (this.started || this.closed) throw new Error('Create a new viewer for a new session');
    this.started = true;
    try {
      const pc = new RTCPeerConnection(this.configuration);
      this.pc = pc;
      const media = new MediaStream(); this.video.srcObject = media; this.video.playsInline = true;
      pc.ontrack = ({track}) => { media.addTrack(track); this.video.play().catch(() => this.state('play-required')); };
      pc.onicecandidate = ({candidate}) => { if (candidate) this.signal({type: 'ice-candidate', candidate: candidate.toJSON()}).catch(e => this.fail(e)); };
      pc.onconnectionstatechange = () => {
        this.state(pc.connectionState);
        if (pc.connectionState === 'failed') this.restart().catch(e => this.fail(e));
      };
      pc.addTransceiver('video', {direction: 'recvonly'});
      // Separate system-audio and target-microphone receive tracks.
      pc.addTransceiver('audio', {direction: 'recvonly'});
      pc.addTransceiver('audio', {direction: 'recvonly'});
      if (microphone) {
        this.microphone = await navigator.mediaDevices.getUserMedia({audio: true});
        if (this.closed) { this.microphone.getTracks().forEach(t => t.stop()); return; }
        for (const track of this.microphone.getTracks()) {
          const sender = pc.addTrack(track, this.microphone);
          const transceiver = pc.getTransceivers().find(t => t.sender === sender);
          const codecs = (RTCRtpSender.getCapabilities('audio')?.codecs ?? []).filter(c => ['audio/opus', 'audio/pcmu'].includes(c.mimeType.toLowerCase()));
          if (!codecs.length) throw new Error('No compatible Opus/PCMU microphone codec');
          transceiver?.setCodecPreferences(codecs);
        }
      }
      if (keyboard || pointer) this.input = pc.createDataChannel('board.input', {ordered: true});
      await this.offer(); void this.poll();
    } catch (error) { this.close(); throw error; }
  }

  private async offer(iceRestart = false): Promise<void> {
    const pc = this.requirePeer();
    const offer = await pc.createOffer({iceRestart}); await pc.setLocalDescription(offer);
    await this.signal({type: 'offer', sdp: offer.sdp});
  }

  async restart(): Promise<void> {
    if (this.closed || this.restarting) return;
    this.restarting = true;
    try { await this.offer(true); } finally { this.restarting = false; }
  }

  private async poll(): Promise<void> {
    try {
      while (!this.closed) {
        const pc = this.requirePeer();
        for (const signal of await this.action('webrtc.poll') as DeviceViewerSignal[]) {
          if (signal.type === 'offer' || signal.type === 'answer') {
            await pc.setRemoteDescription({type: signal.type, sdp: signal.sdp});
            for (const candidate of this.pending.splice(0)) await pc.addIceCandidate(candidate);
            if (signal.type === 'offer') { const answer = await pc.createAnswer(); await pc.setLocalDescription(answer); await this.signal({type: 'answer', sdp: answer.sdp}); }
          } else if (signal.type === 'ice-candidate') {
            if (pc.remoteDescription) await pc.addIceCandidate(signal.candidate);
            else { if (this.pending.length >= 128) throw new Error('ICE queue full'); this.pending.push(signal.candidate); }
          } else if (signal.type === 'bye') { this.close(); return; }
        }
        await new Promise(resolve => setTimeout(resolve, 250));
      }
    } catch (error) { if (!this.closed) this.fail(error); }
  }

  // Atomic keystrokes never hold keys remotely after focus loss/disconnection.
  move(x: number, y: number): void { this.sendInput('pointer.move', {x, y}); if (this.heldPointer) this.heldPointer = {...this.heldPointer, x, y}; }

  down(x: number, y: number, button: PointerButton = 'left'): void {
    if (this.heldPointer) throw new Error('Release the held pointer first');
    this.sendInput('pointer.down', {x, y, button}); this.heldPointer = {x, y, button};
    this.holdTimer = setInterval(() => { const hold = this.heldPointer; if (hold) try { this.move(hold.x, hold.y); } catch (error) { this.fail(error); } }, 1000);
  }

  up(x: number, y: number, button: PointerButton = 'left'): void {
    if (!this.heldPointer) return;
    if (this.heldPointer.button !== button) throw new Error('Held pointer button mismatch');
    try { this.sendInput('pointer.up', {x, y, button}); } catch (error) { this.fail(error); throw error; }
    finally { clearInterval(this.holdTimer); this.heldPointer = null; }
  }

  releasePointer(): void {
    if (!this.heldPointer) return;
    clearInterval(this.holdTimer); this.heldPointer = null;
    try { this.sendInput('pointer.release', {}); } catch (error) { if (!this.closed) this.fail(error); }
  }

  click(x: number, y: number, button: PointerButton = 'left'): void { this.sendInput('pointer.click', {x, y, button}); }
  scroll(x: number, y: number, deltaX: number, deltaY: number): void { this.sendInput('pointer.scroll', {x, y, deltaX, deltaY}); }
  type(text: string): void { this.sendInput('keyboard.type', {text}); }
  press(key: string): void { this.sendInput('keyboard.press', {key}); }

  private sendInput(capability: string, input: unknown): void {
    const channel = this.input;
    if (channel?.readyState !== 'open') throw new Error('Input control is not connected');
    const data = JSON.stringify({capability, input});
    if (new TextEncoder().encode(data).length > 16384 || channel.bufferedAmount > 65536) throw new Error('Input limit reached');
    channel.send(data);
  }

  // Export only route types/counters. Never expose ICE credentials, addresses or SDP.
  async diagnostics(): Promise<DeviceViewerDiagnostics> {
    if (!this.pc || this.closed) throw new Error('No active connection');
    const stats = await this.pc.getStats();
    const routes: DeviceViewerRoute[] = [], media: DeviceViewerMedia[] = [];
    for (const value of stats.values() as IterableIterator<Stats>) {
      if (value.type === 'transport' && value.selectedCandidatePairId) {
        const pair: Stats | undefined = stats.get(value.selectedCandidatePairId);
        const local: Stats | undefined = pair && stats.get(pair.localCandidateId), remote: Stats | undefined = pair && stats.get(pair.remoteCandidateId);
        routes.push({localType: local?.candidateType ?? 'unknown', remoteType: remote?.candidateType ?? 'unknown',
          protocol: local?.protocol, state: pair?.state, bytesSent: pair?.bytesSent, bytesReceived: pair?.bytesReceived});
      }
      if (value.type === 'inbound-rtp' || value.type === 'outbound-rtp') {
        const codec: Stats | undefined = stats.get(value.codecId);
        media.push({direction: value.type, kind: value.kind, codec: codec?.mimeType,
          packetsReceived: value.packetsReceived, packetsSent: value.packetsSent, packetsLost: value.packetsLost,
          jitter: value.jitter, framesDecoded: value.framesDecoded, bytesReceived: value.bytesReceived, bytesSent: value.bytesSent});
      }
    }
    return {time: new Date().toISOString(), connectionState: this.pc.connectionState,
      icePolicy: this.configuration.iceTransportPolicy, routes, media};
  }

  muteMicrophone(muted: boolean): void { this.microphone?.getAudioTracks().forEach(t => { t.enabled = !muted; }); }

  private fail(error: unknown): void { this.state(error instanceof Error ? error.message : String(error)); this.close(); }

  close(): void {
    if (this.closed) return; this.closed = true;
    this.releasePointer(); clearInterval(this.holdTimer);
    window.removeEventListener('blur', this.releaseOnBlur);
    document.removeEventListener('visibilitychange', this.releaseOnHidden);
    // Best-effort remote teardown; host session expiration is the hard bound.
    fetch(this.url, {method: 'POST', credentials: 'omit', headers: {Authorization: `Bearer ${this.token}`, 'Content-Type': 'application/json'}, body: JSON.stringify({capability: 'webrtc.signal', input: {type: 'bye'}}), keepalive: true}).catch(() => {});
    this.abort.abort(); this.microphone?.getTracks().forEach(t => t.stop()); this.pc?.close();
    this.video.srcObject = null; this.token = ''; this.state('closed');
  }

  private requirePeer(): RTCPeerConnection {
    if (!this.pc) throw new Error('No active connection');
    return this.pc;
  }
}

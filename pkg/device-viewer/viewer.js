/** Session-scoped WebRTC viewer. Host supplies a credential from POST
 * /v1/device-sessions/{id}/viewer, never its operator token. */
export class DeviceViewer extends EventTarget {
  constructor({url, token, video, iceServers = [], relayOnly = false}) {
    super();
    const endpoint = new URL(url, location.href);
    if (endpoint.origin !== location.origin) throw new Error('Viewer bridge must be same-origin');
    if (!/^\/v1\/device-viewer\/[a-f0-9]+$/.test(endpoint.pathname)) throw new Error('Invalid viewer endpoint');
    this.url = endpoint.href; this.token = token; this.video = video;
    this.configuration = {iceServers, iceTransportPolicy: relayOnly ? 'relay' : 'all'};
    this.abort = new AbortController(); this.pending = []; this.serial = Promise.resolve();
    this.closed = false; this.started = false;
  }
  async action(capability, input = {}) {
    const response = await fetch(this.url, {method:'POST', credentials:'omit', cache:'no-store',
      headers:{'Authorization':`Bearer ${this.token}`, 'Content-Type':'application/json'},
      body:JSON.stringify({capability,input}), signal:this.abort.signal});
    if (!response.ok) throw new Error(`Device session: HTTP ${response.status}`);
    return (await response.json()).output;
  }
  signal(value) {
    const task = this.serial.then(() => this.action('webrtc.signal', value));
    this.serial = task.catch(() => {}); return task;
  }
  state(value) { this.dispatchEvent(new CustomEvent('state', {detail:value})); }
  async connect({microphone = false, keyboard = false, pointer = false} = {}) {
    if (this.started || this.closed) throw new Error('Create a new viewer for a new session');
    this.started = true;
    try {
      this.pc = new RTCPeerConnection(this.configuration);
      const media = new MediaStream(); this.video.srcObject = media; this.video.playsInline = true;
      this.pc.ontrack = ({track}) => { media.addTrack(track); this.video.play().catch(() => this.state('play-required')); };
      this.pc.onicecandidate = ({candidate}) => { if(candidate) this.signal({type:'ice-candidate',candidate:candidate.toJSON()}).catch(e => this.fail(e)); };
      this.pc.onconnectionstatechange = () => {
        this.state(this.pc.connectionState);
        if(this.pc.connectionState === 'failed') this.restart().catch(e => this.fail(e));
      };
      this.pc.addTransceiver('video', {direction:'recvonly'});
      // Separate system-audio and target-microphone receive tracks.
      this.pc.addTransceiver('audio', {direction:'recvonly'});
      this.pc.addTransceiver('audio', {direction:'recvonly'});
      if (microphone) {
        this.microphone = await navigator.mediaDevices.getUserMedia({audio:true});
        if(this.closed) { this.microphone.getTracks().forEach(t=>t.stop()); return; }
        for(const track of this.microphone.getTracks()) {
          const sender = this.pc.addTrack(track, this.microphone);
          const transceiver = this.pc.getTransceivers().find(t=>t.sender===sender);
          const codecs = RTCRtpSender.getCapabilities('audio').codecs.filter(c=>['audio/opus','audio/pcmu'].includes(c.mimeType.toLowerCase()));
          if(!codecs.length) throw new Error('No compatible Opus/PCMU microphone codec');
          transceiver.setCodecPreferences(codecs);
        }
      }
      if(keyboard || pointer) this.input = this.pc.createDataChannel('board.input',{ordered:true});
      await this.offer(); this.poll();
    } catch(error) { this.close(); throw error; }
  }
  async offer(iceRestart = false) {
    const offer = await this.pc.createOffer({iceRestart}); await this.pc.setLocalDescription(offer);
    await this.signal({type:'offer',sdp:offer.sdp});
  }
  async restart() {
    if(this.closed || this.restarting) return;
    this.restarting = true;
    try { await this.offer(true); } finally { this.restarting = false; }
  }
  async poll() {
    try {
      while(!this.closed) {
        for(const signal of await this.action('webrtc.poll')) {
          if(signal.type === 'offer' || signal.type === 'answer') {
            await this.pc.setRemoteDescription({type:signal.type,sdp:signal.sdp});
            for(const candidate of this.pending.splice(0)) await this.pc.addIceCandidate(candidate);
            if(signal.type === 'offer') { const answer = await this.pc.createAnswer(); await this.pc.setLocalDescription(answer); await this.signal({type:'answer',sdp:answer.sdp}); }
          } else if(signal.type === 'ice-candidate') {
            if(this.pc.remoteDescription) await this.pc.addIceCandidate(signal.candidate);
            else { if(this.pending.length>=128) throw new Error('ICE queue full'); this.pending.push(signal.candidate); }
          } else if(signal.type === 'bye') { this.close(); return; }
        }
        await new Promise(resolve=>setTimeout(resolve,250));
      }
    } catch(error) { if(!this.closed) this.fail(error); }
  }
  // Atomic keystrokes never hold keys remotely after focus loss/disconnection.
  move(x,y) { this.sendInput("pointer.move",{x,y}); }
  click(x,y,button="left") { this.sendInput("pointer.click",{x,y,button}); }
  scroll(x,y,deltaX,deltaY) { this.sendInput("pointer.scroll",{x,y,deltaX,deltaY}); }
  type(text) { this.sendInput('keyboard.type',{text}); }
  press(key) { this.sendInput('keyboard.press',{key}); }
  sendInput(capability,input) {
    if(this.input?.readyState !== 'open') throw new Error('Input control is not connected');
    const data=JSON.stringify({capability,input});
    if(new TextEncoder().encode(data).length > 16384 || this.input.bufferedAmount > 65536) throw new Error('Input limit reached');
    this.input.send(data);
  }
  // Export only route types/counters. Never expose ICE credentials, addresses or SDP.
  async diagnostics() {
    if (!this.pc || this.closed) throw new Error('No active connection');
    const stats = await this.pc.getStats();
    const routes = [], media = [];
    for (const value of stats.values()) {
      if (value.type === 'transport' && value.selectedCandidatePairId) {
        const pair = stats.get(value.selectedCandidatePairId);
        const local = pair && stats.get(pair.localCandidateId), remote = pair && stats.get(pair.remoteCandidateId);
        routes.push({localType:local?.candidateType ?? 'unknown', remoteType:remote?.candidateType ?? 'unknown',
          protocol:local?.protocol, state:pair?.state, bytesSent:pair?.bytesSent, bytesReceived:pair?.bytesReceived});
      }
      if (value.type === 'inbound-rtp' || value.type === 'outbound-rtp') {
        const codec = stats.get(value.codecId);
        media.push({direction:value.type, kind:value.kind, codec:codec?.mimeType,
          packetsReceived:value.packetsReceived, packetsSent:value.packetsSent, packetsLost:value.packetsLost,
          jitter:value.jitter, framesDecoded:value.framesDecoded, bytesReceived:value.bytesReceived, bytesSent:value.bytesSent});
      }
    }
    return {time:new Date().toISOString(), connectionState:this.pc.connectionState,
      icePolicy:this.configuration.iceTransportPolicy, routes, media};
  }
  muteMicrophone(muted) { this.microphone?.getAudioTracks().forEach(t=>{t.enabled=!muted;}); }
  fail(error) { this.state(error.message); this.close(); }
  close() {
    if(this.closed) return; this.closed=true;
    // Best-effort remote teardown; host session expiration is the hard bound.
    fetch(this.url,{method:'POST',credentials:'omit',headers:{Authorization:`Bearer ${this.token}`,'Content-Type':'application/json'},body:JSON.stringify({capability:'webrtc.signal',input:{type:'bye'}}),keepalive:true}).catch(()=>{});
    this.abort.abort(); this.microphone?.getTracks().forEach(t=>t.stop()); this.pc?.close();
    this.video.srcObject=null; this.token=''; this.state('closed');
  }
}

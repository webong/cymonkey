import { bootstrapCameraKit, createMediaStreamSource } from '@snap/camera-kit';
export const CYMONKEY_PROTOCOL_VERSION = 'cymonkey/v1alpha1';
export const CAMERA_KIT_RUNTIME = 'snapchat-camera-kit';
export const CAMERA_KIT_DRIVER = 'sandbox';
export const CYMONKEY_RUNTIME_SYMBOL = Symbol.for('cymonkey.jangolova.runtime');
const actions = [
    'camera-kit.overlay.mount',
    'camera-kit.session.describe',
    'camera-kit.lens.apply',
    'camera-kit.lens.remove',
    'camera-kit.camera.stop',
    'camera-kit.overlay.unmount',
];
export class CameraKitCymonkey {
    protocolVersion = CYMONKEY_PROTOCOL_VERSION;
    augmentationId;
    apiToken;
    title;
    mediaProvider;
    #overlay = null;
    #cameraKit = null;
    #revision = 0;
    constructor(options) {
        this.augmentationId = identifier(options.augmentationId, 'augmentationId');
        if (!options.apiToken.trim())
            throw new Error('Camera Kit API token is required');
        this.apiToken = options.apiToken;
        this.title = options.title || 'Camera Kit';
        this.mediaProvider = options.mediaProvider ?? {
            openCamera: () => navigator.mediaDevices.getUserMedia({ video: true, audio: false }),
            closeCamera: async () => undefined,
        };
    }
    // A target-owned web application installs this after it has imported this
    // package itself. Cymonkey can then attach through its generic page-runtime
    // route without Browser Extension importing Camera Kit.
    installGlobal(target = globalThis) {
        if (target[CYMONKEY_RUNTIME_SYMBOL] && target[CYMONKEY_RUNTIME_SYMBOL] !== this) {
            throw new Error('another Cymonkey runtime is already installed');
        }
        Object.defineProperty(target, CYMONKEY_RUNTIME_SYMBOL, { configurable: true, value: this });
        return () => { if (target[CYMONKEY_RUNTIME_SYMBOL] === this)
            delete target[CYMONKEY_RUNTIME_SYMBOL]; };
    }
    hello() {
        return {
            protocolVersion: this.protocolVersion,
            implementation: { name: 'cymonkey-snapchat-camera-kit', version: '0.1.0' },
            domains: ['render'], runtimes: [CAMERA_KIT_RUNTIME], drivers: [CAMERA_KIT_DRIVER],
            features: ['explicit-user-camera-consent', 'owned-canvas', 'lens-control'],
        };
    }
    capabilities() {
        return actions.map((name) => ({
            name, domain: 'render', runtime: CAMERA_KIT_RUNTIME, driver: CAMERA_KIT_DRIVER,
            support: 'native', lifetime: 'document', persistence: 'ephemeral',
            effect: name.includes('describe') ? 'read' : 'write',
            inputSchema: { type: 'object', additionalProperties: true },
        }));
    }
    describe() {
        const overlay = this.#overlay;
        return {
            revision: String(this.#revision),
            surfaces: overlay ? [{
                    id: `camera-kit:${this.augmentationId}`, domain: 'render', runtime: CAMERA_KIT_RUNTIME,
                    kind: 'canvas', label: this.title,
                    properties: { started: overlay.started, cameraPermission: 'user-gesture-required' },
                    actions,
                }] : [],
            augmentations: [{ id: this.augmentationId, enabled: Boolean(overlay) }],
        };
    }
    async dispatch(request) {
        try {
            const params = record(request.params) ? request.params : {};
            if (request.method === 'hello')
                return response(request.id, this.hello());
            if (request.method === 'capabilities')
                return response(request.id, this.capabilities());
            if (request.method === 'describe')
                return response(request.id, this.describe());
            if (request.method === 'events')
                return response(request.id, { events: [], cursor: '0' });
            if (request.method === 'act')
                return response(request.id, await this.act(params));
            throw new Error(`unsupported Cymonkey method ${JSON.stringify(request.method)}`);
        }
        catch (error) {
            return { id: request.id ?? null, error: { code: 'camera_kit_error', message: errorMessage(error) } };
        }
    }
    async act(params) {
        const name = string(params.name, 'action name');
        const input = record(params.input) ? params.input : {};
        if (name === 'camera-kit.overlay.mount')
            return this.mount(input);
        if (name === 'camera-kit.session.describe')
            return this.describe();
        if (name === 'camera-kit.lens.apply')
            return this.applyLens(input);
        if (name === 'camera-kit.lens.remove')
            return this.removeLens();
        if (name === 'camera-kit.camera.stop')
            return this.stopCamera();
        if (name === 'camera-kit.overlay.unmount')
            return this.unmount();
        throw new Error(`unsupported Camera Kit action ${JSON.stringify(name)}`);
    }
    mount(input) {
        if (this.#overlay)
            throw new Error('Camera Kit overlay is already mounted');
        const host = document.createElement('div');
        host.dataset.jangolovaAugmentation = this.augmentationId;
        const shadow = host.attachShadow({ mode: 'closed' });
        const canvas = document.createElement('canvas');
        const button = document.createElement('button');
        button.type = 'button';
        button.textContent = typeof input.startLabel === 'string' ? input.startLabel : 'Start camera';
        const notice = document.createElement('span');
        notice.textContent = 'Camera access requires your click.';
        const style = document.createElement('style');
        style.textContent = ':host { all: initial; } canvas { display:block; width:100%; height:100%; background:#111; } button { position:absolute; inset:auto 12px 12px auto; } span { position:absolute; inset:12px auto auto 12px; color:white; font:12px system-ui,sans-serif; }';
        shadow.append(style, canvas, button, notice);
        Object.assign(host.style, {
            position: 'fixed', inset: 'auto 24px 24px auto', width: cssLength(input.width, '360px'), height: cssLength(input.height, '640px'),
            zIndex: '2147483647', borderRadius: '12px', overflow: 'hidden', background: '#111', boxShadow: '0 8px 30px rgb(0 0 0 / 35%)',
        });
        (document.documentElement || document.body).append(host);
        const overlay = { host, canvas, session: null, stream: null, started: false };
        button.addEventListener('click', () => void this.startFromUserGesture(overlay, button, notice));
        this.#overlay = overlay;
        this.#revision += 1;
        return { ok: true, userActionRequired: true, surfaceId: `camera-kit:${this.augmentationId}` };
    }
    async applyLens(input) {
        const overlay = this.requireStarted();
        const lensId = string(input.lensId, 'lensId');
        const lensGroupId = string(input.lensGroupId, 'lensGroupId');
        const lens = await this.requireCameraKit().lensRepository.loadLens(lensId, lensGroupId);
        await overlay.session.applyLens(lens);
        this.#revision += 1;
        return { ok: true, lensId, lensGroupId };
    }
    async removeLens() {
        const overlay = this.requireStarted();
        await overlay.session.removeLens();
        this.#revision += 1;
        return { ok: true };
    }
    async stopCamera() {
        const overlay = this.#overlay;
        if (!overlay?.started)
            return { ok: true, stopped: false };
        await overlay.session?.pause?.();
        overlay.stream?.getTracks().forEach((track) => track.stop());
        await this.mediaProvider.closeCamera();
        overlay.stream = null;
        overlay.started = false;
        this.#revision += 1;
        return { ok: true, stopped: true };
    }
    async unmount() {
        if (!this.#overlay)
            return { ok: true, removed: false };
        await this.stopCamera();
        this.#overlay.host.remove();
        this.#overlay = null;
        await this.#cameraKit?.destroy();
        this.#cameraKit = null;
        this.#revision += 1;
        return { ok: true, removed: true };
    }
    async startFromUserGesture(overlay, button, notice) {
        if (overlay.started)
            return;
        button.disabled = true;
        notice.textContent = 'Requesting camera permission…';
        let stream = null;
        try {
            stream = await this.mediaProvider.openCamera();
            const cameraKit = await this.loadCameraKit();
            const session = await cameraKit.createSession({ liveRenderTarget: overlay.canvas });
            await session.setSource(createMediaStreamSource(stream));
            await session.play();
            overlay.stream = stream;
            overlay.session = session;
            overlay.started = true;
            this.#revision += 1;
            notice.textContent = 'Camera is active.';
            button.hidden = true;
        }
        catch (error) {
            stream?.getTracks().forEach((track) => track.stop());
            await this.mediaProvider.closeCamera().catch(() => undefined);
            notice.textContent = `Camera unavailable: ${errorMessage(error)}`;
            button.disabled = false;
        }
    }
    async loadCameraKit() {
        if (!this.#cameraKit)
            this.#cameraKit = await bootstrapCameraKit({ apiToken: this.apiToken });
        return this.#cameraKit;
    }
    requireCameraKit() {
        if (!this.#cameraKit)
            throw new Error('Camera Kit has not started');
        return this.#cameraKit;
    }
    requireStarted() {
        if (!this.#overlay?.started || !this.#overlay.session)
            throw new Error('camera has not been started by the user');
        return this.#overlay;
    }
}
function response(id, result) { return { id: id ?? null, result }; }
function record(value) { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function string(value, name) { if (typeof value !== 'string' || !value.trim())
    throw new Error(`${name} is required`); return value; }
function identifier(value, name) { const result = string(value, name); if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(result))
    throw new Error(`${name} is invalid`); return result; }
function errorMessage(error) { return error instanceof Error ? error.message : String(error); }
function cssLength(value, fallback) { return typeof value === 'string' && /^\d{1,4}(?:\.\d{1,2})?(?:px|vw|vh|%)$/.test(value) ? value : fallback; }

import {
  AmbientLight,
  BoxGeometry,
  Color,
  DirectionalLight,
  Mesh,
  MeshStandardMaterial,
  PerspectiveCamera,
  Scene,
  SphereGeometry,
  WebGLRenderer,
} from 'three';
import {ThreeJSCymonkey, type CymonkeyRequest} from './index';

type FactoryContext = {
  packageId: string;
  augmentationId: string;
  configuration: Record<string, unknown>;
};

type PackageRuntime = {
  packageId: string;
  dispatch(request: unknown): Promise<unknown>;
  unmount(): void;
};

const packageId = 'threejs';
const factoriesSymbol = Symbol.for('jangolova.cymonkey.browser-package.factories');
const globalRecord = globalThis as Record<PropertyKey, unknown>;
let factories = globalRecord[factoriesSymbol];
if (!(factories instanceof Map)) {
  factories = new Map<string, (context: FactoryContext) => PackageRuntime>();
  Object.defineProperty(globalRecord, factoriesSymbol, {configurable: true, value: factories});
}
(factories as Map<string, (context: FactoryContext) => PackageRuntime>).set(packageId, createPackageRuntime);

function createPackageRuntime(context: FactoryContext): PackageRuntime {
  const augmentation = new ThreeJSBrowserAugmentation(context);
  return {
    packageId,
    dispatch: (request) => augmentation.dispatch(request),
    unmount: () => augmentation.unmount(),
  };
}

class ThreeJSBrowserAugmentation {
  readonly #engine = new ThreeJSCymonkey();
  readonly #context: FactoryContext;
  readonly #objects = new Map<string, Mesh>();
  readonly #unregister = new Map<string, () => void>();
  #host: HTMLElement | null = null;
  #renderer: WebGLRenderer | null = null;
  #scene: Scene | null = null;
  #camera: PerspectiveCamera | null = null;
  #frame = 0;

  constructor(context: FactoryContext) {
    this.#context = context;
  }

  async dispatch(value: unknown) {
    const request = isRecord(value) ? value as CymonkeyRequest : {method: ''};
    try {
      if (request.method === 'hello') {
        return response(request, {
          ...this.#engine.hello(),
          implementation: {name: 'jangolova-threejs-browser-augmentation', version: '0.2.0'},
          features: ['owned-canvas', 'owned-scene', 'stable-ids', 'events.cursor'],
        });
      }
      if (request.method === 'capabilities') {
        const base = await this.#engine.dispatch({method: 'capabilities'});
        const baseResult = isRecord(base) && Array.isArray(base.result) ? base.result : [];
        return response(request, [...browserCapabilities(), ...baseResult]);
      }
      if (request.method === 'describe') {
        const description = this.#engine.describe();
        return response(request, {
          ...description,
          augmentations: [{
            id: this.#context.augmentationId,
            package: packageId,
            status: this.#host ? 'mounted' : 'ready',
            owns: ['canvas', 'scene', 'camera', 'objects'],
          }],
        });
      }
      if (request.method === 'act') {
        const params = isRecord(request.params) ? request.params : {};
        const name = requireString(params.name, 'action name');
        const input = isRecord(params.input) ? params.input : {};
        if (name === 'threejs.overlay.mount') return response(request, this.mount(input));
        if (name === 'threejs.overlay.unmount') return response(request, this.unmount());
        if (name === 'threejs.object.add') return response(request, this.addObject(input));
        if (name === 'threejs.object.remove') return response(request, this.removeObject(input));
      }
      return this.#engine.dispatch(request);
    } catch (error) {
      return {
        id: request.id ?? null,
        error: {code: 'invalid_input', message: error instanceof Error ? error.message : String(error)},
      };
    }
  }

  mount(input: Record<string, unknown>) {
    if (this.#host) throw new Error('Three.js overlay is already mounted');
    const host = document.createElement('div');
    host.dataset.jangolovaCymonkeyAugmentation = this.#context.augmentationId;
    const shadow = host.attachShadow({mode: 'closed'});
    const canvas = document.createElement('canvas');
    canvas.setAttribute('aria-label', 'Jangolova Three.js augmentation');
    shadow.append(canvas);
    Object.assign(host.style, {
      position: 'fixed',
      right: '24px',
      bottom: '24px',
      width: cssLength(input.width ?? this.#context.configuration.width, '420px'),
      height: cssLength(input.height ?? this.#context.configuration.height, '280px'),
      zIndex: '2147483646',
      overflow: 'hidden',
      borderRadius: '16px',
      boxShadow: '0 12px 40px rgb(0 0 0 / 40%)',
      background: '#07111f',
      pointerEvents: 'none',
    });
    Object.assign(canvas.style, {display: 'block', width: '100%', height: '100%'});
    (document.documentElement || document.body).append(host);

    const scene = new Scene();
    scene.name = 'Jangolova augmentation scene';
    scene.background = new Color(colorValue(input.background ?? this.#context.configuration.background, '#07111f'));
    const camera = new PerspectiveCamera(50, 1, 0.1, 100);
    camera.name = 'Jangolova augmentation camera';
    camera.position.set(0, 0.4, 4);
    const renderer = new WebGLRenderer({canvas, antialias: true, alpha: false});
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
    scene.add(new AmbientLight(0xffffff, 1.2));
    const key = new DirectionalLight(0xffffff, 2.4);
    key.position.set(2, 3, 4);
    scene.add(key);

    this.#host = host;
    this.#scene = scene;
    this.#camera = camera;
    this.#renderer = renderer;
    this.register('scene:main', 'scene', scene, ['resource.describe']);
    this.register('camera:main', 'camera', camera, ['resource.describe', 'object.transform.set', 'camera.projection.set']);
    this.addObject({id: 'hero', shape: 'box', color: input.color ?? this.#context.configuration.color ?? '#7c9cff'});
    this.resize();
    const renderFrame = () => {
      const hero = this.#objects.get('object:hero');
      if (hero) { hero.rotation.x += 0.006; hero.rotation.y += 0.011; }
      renderer.render(scene, camera);
      this.#frame = requestAnimationFrame(renderFrame);
    };
    renderFrame();
    return {ok: true, augmentationId: this.#context.augmentationId, surfaceId: 'scene:main'};
  }

  unmount() {
    if (this.#frame) cancelAnimationFrame(this.#frame);
    this.#frame = 0;
    for (const object of this.#objects.values()) disposeMesh(object);
    this.#objects.clear();
    this.#renderer?.dispose();
    this.#host?.remove();
    for (const unregister of [...this.#unregister.values()].reverse()) unregister();
    this.#unregister.clear();
    this.#host = null;
    this.#renderer = null;
    this.#scene = null;
    this.#camera = null;
    return {ok: true, augmentationId: this.#context.augmentationId};
  }

  addObject(input: Record<string, unknown>) {
    const scene = this.requireScene();
    const name = objectName(input.id);
    const id = `object:${name}`;
    if (this.#objects.has(id)) throw new Error(`Three.js object ${JSON.stringify(id)} already exists`);
    const shape = input.shape === 'sphere' ? 'sphere' : 'box';
    const geometry = shape === 'sphere' ? new SphereGeometry(0.8, 32, 20) : new BoxGeometry(1.4, 1.4, 1.4);
    const material = new MeshStandardMaterial({color: new Color(colorValue(input.color, '#7c9cff')), roughness: 0.35, metalness: 0.15});
    const object = new Mesh(geometry, material);
    object.name = name;
    scene.add(object);
    this.#objects.set(id, object);
    this.register(id, 'object', object, ['resource.describe', 'object.visibility.set', 'object.transform.set']);
    this.register(`material:${name}`, 'material', material, ['resource.describe', 'material.property.set']);
    return {ok: true, id, materialId: `material:${name}`, shape};
  }

  removeObject(input: Record<string, unknown>) {
    const name = objectName(input.id);
    const id = `object:${name}`;
    const object = this.#objects.get(id);
    if (!object) throw new Error(`Three.js object ${JSON.stringify(id)} does not exist`);
    object.removeFromParent();
    disposeMesh(object);
    this.#objects.delete(id);
    this.#unregister.get(`material:${name}`)?.();
    this.#unregister.delete(`material:${name}`);
    this.#unregister.get(id)?.();
    this.#unregister.delete(id);
    return {ok: true, id};
  }

  register(id: string, kind: 'scene' | 'object' | 'camera' | 'material', target: unknown, actions: string[]) {
    this.#unregister.set(id, this.#engine.register({id, kind, target, actions}));
  }

  resize() {
    if (!this.#host || !this.#renderer || !this.#camera) return;
    const width = this.#host.clientWidth || 420;
    const height = this.#host.clientHeight || 280;
    this.#renderer.setSize(width, height, false);
    this.#camera.aspect = width / height;
    this.#camera.updateProjectionMatrix();
  }

  requireScene() {
    if (!this.#scene) throw new Error('mount the Three.js overlay before adding objects');
    return this.#scene;
  }
}

function browserCapabilities() {
  return [
    packageCapability('threejs.overlay.mount', 'write'),
    packageCapability('threejs.overlay.unmount', 'write'),
    packageCapability('threejs.object.add', 'write'),
    packageCapability('threejs.object.remove', 'write'),
  ];
}

function packageCapability(name: string, effect: 'read' | 'write') {
  return {
    name, domain: 'render', runtime: 'threejs', driver: 'webextension-package',
    support: 'native', lifetime: 'attachment', persistence: 'session', effect,
    inputSchema: {type: 'object', additionalProperties: true},
  };
}

function response(request: CymonkeyRequest, result: unknown) { return {id: request.id ?? null, result}; }
function requireString(value: unknown, name: string) { if (typeof value !== 'string' || !value) throw new Error(`${name} is required`); return value; }
function objectName(value: unknown) {
  const name = requireString(value, 'object id').replace(/^object:/, '');
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(name)) throw new Error('object id is invalid');
  return name;
}
function cssLength(value: unknown, fallback: string) { return typeof value === 'string' && /^\d{2,4}(?:px|vw|vh|%)$/.test(value) ? value : fallback; }
function colorValue(value: unknown, fallback: string) { return typeof value === 'string' || typeof value === 'number' ? value : fallback; }
function disposeMesh(object: Mesh) {
  object.geometry.dispose();
  const materials = Array.isArray(object.material) ? object.material : [object.material];
  for (const material of materials) material.dispose();
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }

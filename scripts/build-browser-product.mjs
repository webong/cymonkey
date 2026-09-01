import {cp, mkdir, readFile, stat, writeFile} from 'node:fs/promises';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
const productPath = process.argv[2];
if (!productPath || !safeRepositoryPath(productPath) || !productPath.endsWith('.json')) {
  throw new Error('usage: node scripts/build-browser-product.mjs products/browser/<product>.json');
}
const product = validateProduct(await json(`${root}${productPath}`));
const extension = `${root}pkg/browser-ext`;
const packages = [];

for (const entry of product.packages) {
  const directory = `${root}${entry.directory}`;
  const manifest = validatePackage(await json(`${directory}/browser-package.json`));
  run('npm', ['--prefix', directory, 'run', entry.build]);
  const artifacts = new Map();
  for (const delivery of manifest.spec.deliveries) {
    const artifact = `${directory}/dist/${delivery.entrypoint}`;
    if ((await stat(artifact)).size < 100) throw new Error(`${manifest.metadata.id} ${delivery.entrypoint} bundle is unexpectedly small`);
    artifacts.set(delivery.entrypoint, artifact);
  }
  packages.push({directory, artifacts, manifest});
}

for (const browserName of product.browsers) {
  run('npm', ['--prefix', extension, 'run', `build:${browserName}`]);
  const output = `${extension}/.output/${browserName}-mv3`;
  const manifestPath = `${output}/manifest.json`;
  const extensionManifest = await json(manifestPath);
  extensionManifest.name = product.name;
  if (extensionManifest.action) extensionManifest.action.default_title = product.name;
  const registry = {apiVersion: 'cymonkey.browser-package/v1alpha1', kind: 'BrowserPackageRegistry', packages: []};
  const included = packages.filter((item) => item.manifest.spec.deliveries.some((delivery) => delivery.browsers.includes(browserName)));
  for (const item of included) {
    const id = item.manifest.metadata.id;
    const destination = `${output}/augmentations/${id}`;
    await mkdir(destination, {recursive: true});
    const deliveries = item.manifest.spec.deliveries.filter((delivery) => delivery.browsers.includes(browserName));
    for (const delivery of deliveries) await cp(item.artifacts.get(delivery.entrypoint), `${destination}/${delivery.entrypoint}`);
    await writeFile(`${destination}/manifest.json`, `${JSON.stringify(item.manifest, null, 2)}\n`);
    registry.packages.push({id, manifest: `augmentations/${id}/manifest.json`});
  }
  await mkdir(`${output}/augmentations`, {recursive: true});
  await writeFile(`${output}/augmentations/registry.json`, `${JSON.stringify(registry, null, 2)}\n`);
  const sandboxIncluded = included.filter((item) => item.manifest.spec.deliveries.some((delivery) => delivery.kind === 'sandbox' && delivery.browsers.includes(browserName)));
  if (sandboxIncluded.length) {
    if (!extensionManifest.sandbox?.pages?.includes('runtime-sandbox.html')) {
      throw new Error(`${browserName} cannot compose sandbox packages; use page-runtime or augmentation-package delivery`);
    }
    extensionManifest.content_security_policy = {
      ...(extensionManifest.content_security_policy || {}),
      sandbox: sandboxCSP(sandboxIncluded.map((item) => item.manifest)),
    };
  }
  await writeFile(manifestPath, `${JSON.stringify(extensionManifest)}\n`);
  process.stdout.write(`composed ${product.id} for ${browserName} with ${included.length} reviewed package(s)\n`);
}

function validateProduct(value) {
  if (!record(value) || value.apiVersion !== 'cymonkey.browser-package/v1alpha1' || value.kind !== 'BrowserExtensionProduct'
    || !record(value.metadata) || !identifier(value.metadata.id) || typeof value.metadata.name !== 'string' || !record(value.spec)
    || !Array.isArray(value.spec.browsers) || value.spec.browsers.length === 0
    || value.spec.browsers.some((item) => !['chrome', 'edge', 'firefox', 'safari'].includes(item))
    || !Array.isArray(value.spec.packages)) throw new Error('browser product manifest is invalid');
  const entries = value.spec.packages.map((item) => {
    if (!record(item) || typeof item.directory !== 'string' || !safeRepositoryPath(item.directory)
      || typeof item.build !== 'string' || !/^[A-Za-z0-9:_-]+$/.test(item.build)) throw new Error('browser product package entry is invalid');
    return {directory: item.directory, build: item.build};
  });
  return {id: value.metadata.id, name: value.metadata.name, browsers: [...new Set(value.spec.browsers)], packages: entries};
}

function validatePackage(value) {
  if (!record(value) || value.apiVersion !== 'cymonkey.browser-package/v1alpha1' || value.kind !== 'BrowserAugmentationPackage'
    || !record(value.metadata) || !identifier(value.metadata.id) || typeof value.metadata.name !== 'string'
    || typeof value.metadata.version !== 'string' || !record(value.spec) || !Array.isArray(value.spec.deliveries)
    || !Array.isArray(value.spec.permissions) || !Array.isArray(value.spec.capabilities)) throw new Error('browser package manifest is invalid');
  const deliveries = value.spec.deliveries.map((delivery) => {
    const browsers = delivery?.kind === 'sandbox' ? ['chrome', 'edge'] : ['chrome', 'edge', 'firefox', 'safari'];
    if (!record(delivery) || !['sandbox', 'augmentation-package'].includes(delivery.kind)
      || delivery.entrypoint !== (delivery.kind === 'sandbox' ? 'sandbox.js' : 'content.js')
      || !Array.isArray(delivery.browsers) || delivery.browsers.length === 0
      || delivery.browsers.some((item) => !browsers.includes(item))) {
      throw new Error(`browser package ${value.metadata.id} has an invalid delivery`);
    }
    return {kind: delivery.kind, entrypoint: delivery.entrypoint, browsers: [...new Set(delivery.browsers)]};
  });
  const sandbox = record(value.spec.sandbox) ? value.spec.sandbox : {};
  if (value.spec.permissions.some((permission) => permission !== 'camera')) {
    throw new Error(`browser package ${value.metadata.id} requests an unsupported permission`);
  }
  if (value.spec.capabilities.some((capability) => typeof capability !== 'string' || !/^[a-z][a-z0-9.-]{0,127}$/.test(capability))) {
    throw new Error(`browser package ${value.metadata.id} has an invalid capability`);
  }
  const launch = value.spec.launch === undefined ? undefined : validateLaunch(value.spec.launch, value.spec.capabilities, value.metadata.id);
  for (const list of [sandbox.scriptSources ?? [], sandbox.connectSources ?? []]) {
    if (!Array.isArray(list) || list.some((source) => !safeCSPSource(source))) throw new Error(`browser package ${value.metadata.id} has an invalid CSP source`);
  }
  return {
    apiVersion: value.apiVersion, kind: value.kind,
    metadata: {id: value.metadata.id, name: value.metadata.name, version: value.metadata.version},
    spec: {
      deliveries,
      permissions: [...new Set(value.spec.permissions)],
      capabilities: [...new Set(value.spec.capabilities)],
      ...(launch ? {launch} : {}),
      sandbox: {wasm: sandbox.wasm === true, scriptSources: sandbox.scriptSources ?? [], connectSources: sandbox.connectSources ?? []},
    },
  };
}

function validateLaunch(value, capabilities, packageId) {
  if (!record(value) || typeof value.name !== 'string' || !/^[a-z][a-z0-9.-]{0,127}$/.test(value.name)
    || value.input !== undefined && !record(value.input) || !capabilities.includes(value.name)) {
    throw new Error(`browser package ${packageId} has an invalid launch action`);
  }
  return {name: value.name, input: value.input ?? {}};
}

function sandboxCSP(manifests) {
  const scriptSources = new Set(["'self'"]);
  const connectSources = new Set(["'self'"]);
  let wasm = false;
  for (const manifest of manifests) {
    for (const source of manifest.spec.sandbox.scriptSources) scriptSources.add(source);
    for (const source of manifest.spec.sandbox.connectSources) connectSources.add(source);
    wasm ||= manifest.spec.sandbox.wasm;
  }
  if (wasm) scriptSources.add("'wasm-unsafe-eval'");
  return `sandbox allow-scripts; script-src ${[...scriptSources].join(' ')}; connect-src ${[...connectSources].join(' ')}; object-src 'none'; base-uri 'none';`;
}

function safeRepositoryPath(value) {
  return typeof value === 'string' && !value.startsWith('/') && !value.includes('..') && /^[A-Za-z0-9._/-]+$/.test(value);
}

function safeCSPSource(value) {
  return typeof value === 'string' && value.length <= 256
    && (value === 'blob:' || /^https:\/\/(?:\*\.)?[A-Za-z0-9.-]+(?::\d+)?$/.test(value));
}

function identifier(value) { return typeof value === 'string' && /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(value); }
function record(value) { return typeof value === 'object' && value !== null && !Array.isArray(value); }
async function json(path) { return JSON.parse(await readFile(path, 'utf8')); }
function run(command, args) {
  const result = spawnSync(command, args, {cwd: root, stdio: 'inherit'});
  if (result.status !== 0) throw new Error(`${command} ${args.join(' ')} failed`);
}

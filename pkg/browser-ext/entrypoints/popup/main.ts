const statusElement = requireElement('status');

type Approval = {
  id: string;
  packageName: string;
  packageId: string;
  permissions: string[];
  origin: string;
  expiresAt: string;
};

type ReviewedPackage = {
  id: string;
  name: string;
  version: string;
  permissions: string[];
  capabilities: string[];
  launch?: {name: string; input: Record<string, unknown>};
};

type PendingMount = {packageValue: ReviewedPackage; input: Record<string, unknown>};
const pendingMounts = new Map<string, PendingMount>();

void Promise.all([
  browser.runtime.sendMessage({ channel: 'jangolova.cymonkey.control', method: 'describe', params: {} }),
  browser.runtime.sendMessage({ channel: 'jangolova.extension.control', method: 'describe', params: {} }),
  readApprovals(),
  readPackages(),
]).then(([cymonkeyValue, extensionValue, approvals, packages]) => {
  const cymonkey = cymonkeyValue as {
    extension?: { browser?: string };
    registeredScripts?: string[];
    dynamicRuleIds?: number[];
  };
  const extension = extensionValue as {
    distribution?: string;
    integrations?: { xalletSpook?: { status?: string }; outboundControl?: { status?: string } };
  };
  statusElement.textContent = 'Ready';
  requireElement('distribution').textContent = extension.distribution || 'single-build';
  requireElement('spook').textContent = extension.integrations?.xalletSpook?.status || 'unavailable';
  requireElement('outbound').textContent = extension.integrations?.outboundControl?.status || 'disabled';
  requireElement('browser').textContent = cymonkey.extension?.browser || 'unknown';
  requireElement('scripts').textContent = String(cymonkey.registeredScripts?.length || 0);
  requireElement('rules').textContent = String(cymonkey.dynamicRuleIds?.length || 0);
  renderPackages(packages);
  renderApprovals(approvals);
}).catch((error) => {
  statusElement.textContent = error instanceof Error ? error.message : String(error);
});

async function readApprovals() {
  return browser.runtime.sendMessage({channel: 'jangolova.extension.control', method: 'approval.list', params: {}}) as Promise<Approval[]>;
}

async function readPackages() {
  return browser.runtime.sendMessage({channel: 'jangolova.extension.control', method: 'packages.list', params: {}}) as Promise<ReviewedPackage[]>;
}

function renderPackages(packages: ReviewedPackage[]) {
  const container = requireElement('packages');
  requireElement('packages-empty').hidden = packages.length > 0;
  container.replaceChildren();
  for (const packageValue of packages) {
    const article = document.createElement('article');
    const title = document.createElement('h3');
    title.textContent = `${packageValue.name} ${packageValue.version}`;
    const detail = document.createElement('p');
    detail.className = 'package-capabilities';
    detail.textContent = `${packageValue.permissions.length ? `Permissions: ${packageValue.permissions.join(', ')}. ` : ''}${packageValue.capabilities.join(', ')}`;
    const configuration = document.createElement('textarea');
    configuration.setAttribute('aria-label', `${packageValue.name} configuration JSON`);
    configuration.placeholder = '{"configuration":"value"}';
    configuration.spellcheck = false;
    const actions = document.createElement('div');
    actions.className = 'package-actions';
    const mount = packageButton('Mount on active tab', 'mount');
    const unmount = packageButton('Unmount', 'unmount');
    mount.addEventListener('click', () => void runButton(mount, async () => {
      await mountPackage(packageValue, createMountInput(packageValue, parseConfiguration(configuration.value)));
    }));
    unmount.addEventListener('click', () => void runButton(unmount, async () => {
      await cymonkeyAct('sandbox.unmount', {
        augmentationId: augmentationID(packageValue), id: sandboxID(packageValue),
      });
      statusElement.textContent = `${packageValue.name} unmounted.`;
    }));
    actions.append(mount, unmount);
    article.append(title, detail, configuration, actions);
    container.append(article);
  }
}

function packageButton(label: string, className: 'mount' | 'unmount') {
  const button = document.createElement('button');
  button.type = 'button';
  button.textContent = label;
  button.className = className;
  return button;
}

async function runButton(button: HTMLButtonElement, operation: () => Promise<void>) {
  button.disabled = true;
  try {
    await operation();
  } catch (error) {
    statusElement.textContent = error instanceof Error ? error.message : String(error);
  } finally {
    button.disabled = false;
  }
}

function createMountInput(packageValue: ReviewedPackage, configuration: Record<string, unknown>) {
  return {
    augmentationId: augmentationID(packageValue),
    id: sandboxID(packageValue),
    package: packageValue.id,
    permissions: packageValue.permissions,
    configuration,
    title: packageValue.name,
  };
}

async function mountPackage(packageValue: ReviewedPackage, input: Record<string, unknown>, approvalId?: string) {
  const result = await cymonkeyAct('sandbox.mount', {...input, ...(approvalId ? {approvalId} : {})}) as {
    ok?: boolean;
    status?: string;
    approval?: Approval;
  };
  if (result.status === 'approval-required' && result.approval) {
    pendingMounts.set(result.approval.id, {packageValue, input});
    renderApprovals(await readApprovals());
    statusElement.textContent = `Approve ${packageValue.name} below to continue.`;
    return;
  }
  if (packageValue.launch) {
    await extensionCall('cymonkey-engine.call', {
      augmentationId: augmentationID(packageValue),
      sandboxId: sandboxID(packageValue),
      delivery: 'sandbox',
      request: {method: 'act', params: {name: packageValue.launch.name, input: packageValue.launch.input}},
    });
  }
  statusElement.textContent = `${packageValue.name} mounted on the active tab.`;
}

function cymonkeyAct(name: string, input: Record<string, unknown>) {
  return extensionCall('cymonkey.call', {method: 'act', params: {name, input}});
}

function extensionCall(method: string, params: Record<string, unknown>) {
  return browser.runtime.sendMessage({channel: 'jangolova.extension.control', method, params});
}

function parseConfiguration(source: string) {
  if (!source.trim()) return {};
  const value: unknown = JSON.parse(source);
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('package configuration must be a JSON object');
  return value as Record<string, unknown>;
}

function augmentationID(packageValue: ReviewedPackage) { return `standalone.${packageValue.id}`; }
function sandboxID(packageValue: ReviewedPackage) { return `standalone-${packageValue.id}`; }

function renderApprovals(approvals: Approval[]) {
  const container = requireElement('approvals');
  const empty = requireElement('approvals-empty');
  container.replaceChildren();
  empty.hidden = approvals.length > 0;
  for (const approval of approvals) {
    const article = document.createElement('article');
    const title = document.createElement('h3');
    title.textContent = approval.packageName;
    const detail = document.createElement('p');
    detail.textContent = `${approval.permissions.join(', ')} on ${approval.origin}`;
    const actions = document.createElement('div');
    actions.className = 'approval-actions';
    actions.append(approvalButton('Allow once', 'approve', approval), approvalButton('Deny', 'deny', approval));
    article.append(title, detail, actions);
    container.append(article);
  }
}

function approvalButton(label: string, decision: 'approve' | 'deny', approval: Approval) {
  const button = document.createElement('button');
  button.type = 'button';
  button.textContent = label;
  button.className = decision;
  button.addEventListener('click', async () => {
    button.disabled = true;
    try {
      await browser.runtime.sendMessage({channel: 'jangolova.extension.control', method: 'approval.resolve', params: {id: approval.id, decision}});
      const pending = pendingMounts.get(approval.id);
      pendingMounts.delete(approval.id);
      renderApprovals(await readApprovals());
      if (decision === 'approve' && pending) {
        await mountPackage(pending.packageValue, pending.input, approval.id);
      } else {
        statusElement.textContent = decision === 'approve' ? 'Approved once — the original caller can retry.' : 'Request denied.';
      }
    } catch (error) {
      statusElement.textContent = error instanceof Error ? error.message : String(error);
      button.disabled = false;
    }
  });
  return button;
}

function requireElement(id: string) {
  const element = document.getElementById(id);
  if (!element) throw new Error(`missing popup element ${id}`);
  return element;
}

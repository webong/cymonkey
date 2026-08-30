const statusElement = requireElement('status');

type Approval = {
  id: string;
  packageName: string;
  packageId: string;
  permissions: string[];
  origin: string;
  expiresAt: string;
};

void Promise.all([
  browser.runtime.sendMessage({ channel: 'jangolova.cymonkey.control', method: 'describe', params: {} }),
  browser.runtime.sendMessage({ channel: 'jangolova.extension.control', method: 'describe', params: {} }),
  readApprovals(),
]).then(([cymonkeyValue, extensionValue, approvals]) => {
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
  renderApprovals(approvals);
}).catch((error) => {
  statusElement.textContent = error instanceof Error ? error.message : String(error);
});

async function readApprovals() {
  return browser.runtime.sendMessage({channel: 'jangolova.extension.control', method: 'approval.list', params: {}}) as Promise<Approval[]>;
}

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
      renderApprovals(await readApprovals());
      statusElement.textContent = decision === 'approve' ? 'Approved once — the caller can retry.' : 'Request denied.';
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

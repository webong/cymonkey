import { privilegedCapabilityNames } from '../src/capabilities';
import { dispatchCymonkey } from '../src/engine';
import { OutboundControlClient } from '../src/outbound-control';
import { dispatchJangolova, setOutboundControlStatus, setXalletSpookStatus } from '../src/runtime';
import { publishAuditEvent, publishCymonkeyEvent } from '../src/services/events';
import { ControlPolicyService, isExtensionControlCall, type AuthorizationResult, type ControlSource } from '../src/services/policy';
import { errorMessage, isRecord, type XalletSpookState } from '../src/types';
import { XalletSpookClient } from '../src/xallet-spook';
import { reconcileUserscripts } from '../src/services/userscripts';
import { listPackageApprovals, resolvePackageApproval } from '../src/services/approvals';

export default defineBackground(() => {
  const policy = new ControlPolicyService();
  let state: XalletSpookState = {
    status: 'ready',
    xalletSpook: 'discovering',
    browser: import.meta.env.BROWSER,
    capabilities: privilegedCapabilityNames,
    extensionId: browser.runtime.id,
    outboundControl: 'disabled',
  };
  const spook = new XalletSpookClient(
    'Jangolova Browser Extension',
    state,
    'popup.html',
    (xalletSpook) => {
      state = { ...state, xalletSpook };
      setXalletSpookStatus(xalletSpook);
    },
    browser,
  );
  const controlStorage = browser.storage.session ?? browser.storage.local;
  const outbound = new OutboundControlClient(
    controlStorage,
    handleControlCall,
    (outboundControl) => {
      state = {...state, outboundControl};
      setOutboundControlStatus(outboundControl);
    },
  );
  spook.start();
  void outbound.start();
  void reconcileUserscripts();

  browser.runtime.onInstalled.addListener(() => {
    void reconcileUserscripts();
  });

  browser.runtime.onMessage.addListener((message, sender) => {
    return handleMessage(message, sender.tab?.id);
  });

  browser.runtime.onMessageExternal?.addListener((message, sender) => {
    if (!isExtensionControlCall(message)) return undefined;
    if (!spook.acceptsExternalSender(sender.id)) {
      return Promise.resolve({ ok: false, error: 'external caller is not the registered Xallet browser hub' });
    }
    return handleControlCall(message, 'xallet-spook');
  });

  async function handleMessage(message: unknown, senderTabId?: number) {
    if (!isRecord(message)) return undefined;
    if (message.channel === 'jangolova.cymonkey.event') {
      const event = isRecord(message.event) ? message.event : {};
      return publishCymonkeyEvent(
        typeof event.type === 'string' ? event.type : 'cymonkey.event',
        isRecord(event.data) ? event.data : {},
        senderTabId,
      );
    }
    if (message.channel === 'jangolova.cymonkey.control') {
		return handleExtensionOriginCall({
			type: 'JANGOLOVA_EXTENSION_CALL', method: 'cymonkey.call',
			params: {method: String(message.method || ''), params: isRecord(message.params) ? message.params : {}},
		});
    }
    if (message.channel === 'jangolova.extension.control') {
      return handleExtensionOriginCall({type: 'JANGOLOVA_EXTENSION_CALL', method: String(message.method || ''), params: isRecord(message.params) ? message.params : {}});
    }
    if (message.type === 'GET_SPOKE_STATE') return state;
    if (isExtensionControlCall(message)) return handleControlCall(message, 'extension-origin');
    return undefined;
  }

  async function handleExtensionOriginCall(message: Record<string, unknown>) {
    const response = await handleControlCall(message, 'extension-origin');
    if (response.ok !== true) throw new Error(String(response.error || 'extension control call failed'));
    return response.result;
  }

  async function handleControlCall(message: Record<string, unknown>, source: ControlSource): Promise<Record<string, unknown>> {
    const method = String(message.method || '');
    const params = isRecord(message.params) ? message.params : {};
    let authorization: AuthorizationResult;
    try {
      authorization = await policy.authorize(source, message);
    } catch {
      await publishAuditEvent('denied', {source, capability: method, reason: 'invalid-authorization-context'});
      return {ok: false, error: 'control call could not be authorized'};
    }
    const audit = auditData(authorization);
    await publishAuditEvent('requested', audit);
    if (authorization.decision !== 'allow') {
      await publishAuditEvent('denied', {...audit, reason: 'policy-denied'});
      return {ok: false, error: `control policy denied ${JSON.stringify(authorization.capability)}`};
    }
    state = { ...state, status: 'running', lastAction: method, lastError: undefined };
    await spook.updateState(state);
    try {
      const result = await dispatchAuthorized(message, method, params, source);
      state = { ...state, status: 'ready', lastAction: method, lastError: undefined };
      await spook.updateState(state);
      await publishAuditEvent('succeeded', audit);
      return { ok: true, result };
    } catch (error) {
      state = { ...state, status: 'failed', lastAction: method, lastError: errorMessage(error) };
      await spook.updateState(state);
      await publishAuditEvent('failed', {...audit, reason: 'dispatch-failed'});
      return { ok: false, error: state.lastError };
    }
  }

  async function dispatchAuthorized(message: Record<string, unknown>, method: string, params: Record<string, unknown>, source: ControlSource) {
	if (method === 'cymonkey.call') return dispatchCymonkey(String(params.method || ''), isRecord(params.params) ? params.params : {});
    if (method === 'approval.list' || method === 'approval.resolve') {
      if (source !== 'extension-origin') throw new Error('package approvals are available only in Jangolova extension UI');
      return method === 'approval.list' ? listPackageApprovals() : resolvePackageApproval(params.id, params.decision);
    }
    if (method === 'policy.describe') return policy.describe();
    if (method === 'policy.replace') return policy.replace(params.policy);
    if (method === 'control.websocket.describe') return outbound.describe();
    if (method === 'control.websocket.configure') return outbound.configure(params.configuration);
    if (method === 'control.websocket.disable') return outbound.disable();
    return dispatchJangolova(method, params);
  }

  function auditData(value: AuthorizationResult) {
    return Object.fromEntries(Object.entries({
      source: value.source,
      capability: value.capability,
      effect: value.effect,
      decision: value.decision,
      ruleId: value.ruleId,
      policyMode: value.policyMode,
      tabId: value.tabId,
      origin: value.origin,
      augmentationId: value.augmentationId,
    }).filter(([, item]) => item !== undefined));
  }
});

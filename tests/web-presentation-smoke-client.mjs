#!/usr/bin/env node
import assert from "node:assert/strict";
import { rename, writeFile } from "node:fs/promises";
import process from "node:process";

const providerURL = process.env.JANGOLOVA_PROVIDER_URL || "http://127.0.0.1:7392";
const token = process.env.CYMONKEY_DISPLAY_TOKEN;
const cdpURL = process.env.PRESENTATION_CDP_URL || "http://127.0.0.1:9224";
const sourceURL = process.env.PRESENTATION_SOURCE_URL || "http://127.0.0.1:8081/";
const authenticatedCDPBase = process.env.PRESENTATION_AUTHENTICATED_CDP_BASE;
const credentialRef = process.env.PRESENTATION_CREDENTIAL_REF;
const credentialMaterialPath = process.env.PRESENTATION_CREDENTIAL_MATERIAL_PATH;
const relayAuthorizationPath = process.env.PRESENTATION_RELAY_AUTHORIZATION_PATH;
const rotatedAuthorization = process.env.PRESENTATION_ROTATED_AUTHORIZATION;
const instanceID = "presentation-smoke";

assert.ok(token, "CYMONKEY_DISPLAY_TOKEN is required");

let targetCDPURL = cdpURL;
if (authenticatedCDPBase) {
  const discovery = await fetch(`${cdpURL.replace(/\/$/, "")}/json/version`).then((response) => response.json());
  const discovered = new URL(discovery.webSocketDebuggerUrl);
  const relay = new URL(authenticatedCDPBase);
  relay.pathname = discovered.pathname;
  relay.search = discovered.search;
  targetCDPURL = relay.toString();
}

async function request(path, { method = "GET", body } = {}) {
  const response = await fetch(`${providerURL}${path}`, {
    method,
    headers: {
      authorization: `Bearer ${token}`,
      ...(body === undefined ? {} : { "content-type": "application/json" }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  const value = text ? JSON.parse(text) : undefined;
  if (!response.ok) {
    throw new Error(`${method} ${path} returned ${response.status}: ${text}`);
  }
  return { status: response.status, value };
}

async function call(method, params) {
  const { value } = await request(`/v1/display-instances/${instanceID}/call`, {
    method: "POST",
    body: { method, params },
  });
  return value.result;
}

const { value: connected } = await request("/v1/display-instances", {
  method: "POST",
  body: {
    apiVersion: "cymonkey.display/v1alpha1",
    instanceId: instanceID,
    engine: {
      adapter: "auto",
      requiredCapabilities: ["presentation.mount"],
      source: sourceURL,
      options: {
        policy: {
          maxHTMLBytes: 4096,
          maxCSSBytes: 2048,
          maxJavaScriptBytes: 2048,
          maxTotalBytes: 8192,
          allowedSourceOrigins: ["http://127.0.0.1:8081", "http://127.0.0.1:8082"],
          allowedAssetOrigins: ["self"],
          allowedArtifactTransports: ["http"],
        },
      },
    },
    target: {
      apiVersion: "interaction.target/v1alpha1",
      targetId: "direct-container-chromium",
      kind: "browser",
      endpoints: [{
        name: "cdp",
        protocol: "cdp",
        url: targetCDPURL,
        ...(credentialRef ? { credentialRef } : {}),
        audience: "engine",
        metadata: { "network.scope": "container-private" },
      }],
      metadata: { "owner.kind": "container-supervisor" },
    },
  },
});
assert.equal(connected.status, "connected");
assert.equal(connected.adapter, "web-presentation");
for (const capability of ["presentation.write", "presentation.mount", "artifact.kind.web.entrypoint", "artifact.transport.http", "presentation.capture", "events"]) {
  assert.ok(connected.capabilities.includes(capability), `missing ${capability}`);
}

if (credentialMaterialPath && relayAuthorizationPath && rotatedAuthorization) {
  const rotatedDocument = {
    apiVersion: "cymonkey.connection-material/v1alpha1",
    kind: "credential",
    headers: { Authorization: rotatedAuthorization },
    expiresAt: new Date(Date.now() + 300000).toISOString(),
  };
  const temporaryPath = `${credentialMaterialPath}.next`;
  await writeFile(temporaryPath, JSON.stringify(rotatedDocument), { mode: 0o600 });
  await writeFile(relayAuthorizationPath, `${rotatedAuthorization}\n`, { mode: 0o600 });
  await rename(temporaryPath, credentialMaterialPath);
  const deadline = Date.now() + 8000;
  let renewed = false;
  while (!renewed && Date.now() < deadline) {
    const { value: batch } = await request(`/v1/display-instances/${instanceID}/events?after=0&limit=32`);
    renewed = batch.events.some((event) => event.type === "interaction.connection.renewed");
    if (!renewed) await new Promise((resolve) => setTimeout(resolve, 100));
  }
  assert.equal(renewed, true, "credential lease did not reconnect the CDP worker");
}

const write = await call("act", {
  name: "presentation.write",
  input: {
    expectedStateRevision: "0",
    html: '<article id="authored-card"><h1>Authored live</h1><button id="advance">Advance</button><img id="blocked-asset" src="http://127.0.0.1:8082/pixel.svg" alt=""></article>',
    css: "#authored-card { width: 420px; padding: 24px; background: rgb(20, 40, 80); }",
    js: "root.querySelector('#advance').addEventListener('click', () => emit('advance.clicked', { step: 2 }));",
  },
});
assert.equal(write.ok, true);
assert.equal(write.stateRevision, "1");
assert.equal("document" in write, false, "mutation receipt must not echo the presentation document");

await assert.rejects(
  call("act", {
    name: "presentation.write",
    input: { expectedStateRevision: "0", html: "<p>stale overwrite</p>" },
  }),
  /revision conflict/,
);

await new Promise((resolve) => setTimeout(resolve, 200));

const inspection = await call("act", {
  name: "presentation.execute",
  input: {
    code: "return { heading: root.querySelector('h1')?.textContent, button: root.querySelector('button')?.textContent, cardCount: root.querySelectorAll('#authored-card').length, blockedAssetWidth: root.querySelector('#blocked-asset')?.naturalWidth };",
  },
});
assert.deepEqual(inspection.result, {
  heading: "Authored live",
  button: "Advance",
  cardCount: 1,
  blockedAssetWidth: 0,
});

const batch = await call("events", {
  after: "0",
  types: ["presentation.write"],
  limit: 10,
});
assert.equal(batch.events.length, 1);
assert.equal(batch.events[0].type, "presentation.write");
assert.ok(batch.events[0].data.bytes > 0);

const capture = await call("act", {
  name: "presentation.capture",
  input: { fullPage: true },
});
const png = Buffer.from(capture.pngBase64, "base64");
assert.ok(png.length > 1000, `PNG is unexpectedly small: ${png.length} bytes`);
assert.deepEqual([...png.subarray(0, 8)], [137, 80, 78, 71, 13, 10, 26, 10]);

const mounted = await call("act", {
  name: "presentation.mount",
  input: {
    expectedStateRevision: "1",
    artifact: {
      apiVersion: "interaction.presentation/v1alpha1",
      artifactId: "direct-container-experience",
      revision: "sha256:fixture-v2",
      kind: "web.entrypoint",
      locations: [
        { transport: "provider-handle", uri: "artifact://fixture/experience" },
        { transport: "http", uri: "http://127.0.0.1:8082/", audience: "target" },
      ],
    },
  },
});
assert.deepEqual(mounted, {
  ok: true,
  artifactId: "direct-container-experience",
  artifactRevision: "sha256:fixture-v2",
  stateRevision: "0",
  location: { transport: "http" },
});

const mountedWrite = await call("act", {
  name: "presentation.write",
  input: {
    expectedStateRevision: "0",
    html: "<h1>Mounted in a direct container</h1>",
  },
});
assert.equal(mountedWrite.stateRevision, "1");

const disconnected = await request(`/v1/display-instances/${instanceID}`, { method: "DELETE" });
assert.equal(disconnected.status, 204);

process.stdout.write("Direct-container artifact mount, policy, revision, DOM, capture, and disconnect checks passed\n");
